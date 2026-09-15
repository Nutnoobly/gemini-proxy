package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gemini-proxy/pkg/prompt"
	"gemini-proxy/pkg/worker"
)

// Server handles OpenAI-compatible HTTP endpoints.
type Server struct {
	pool           *worker.WorkerPool
	activeRequests atomic.Int64
	lastActivity   atomic.Int64 // UnixNano
	onActivity     func()
}

// NewServer creates a new API Server backed by the worker pool.
func NewServer(pool *worker.WorkerPool) *Server {
	s := &Server{pool: pool}
	s.lastActivity.Store(time.Now().UnixNano())
	return s
}

// SetOnActivity sets an optional callback triggered on chat activity.
func (s *Server) SetOnActivity(fn func()) {
	s.onActivity = fn
}

// ActiveRequests returns number of currently processing requests.
func (s *Server) ActiveRequests() int64 {
	return s.activeRequests.Load()
}

// LastActivity returns the timestamp of the last incoming chat request.
func (s *Server) LastActivity() time.Time {
	nanos := s.lastActivity.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// Touch marks current time as active activity.
func (s *Server) Touch() {
	s.lastActivity.Store(time.Now().UnixNano())
}

func generateCompletionID() string {
	bytes := make([]byte, 12)
	_, _ = rand.Read(bytes)
	return "chatcmpl-" + hex.EncodeToString(bytes)
}

// Routes sets up the HTTP router.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// OpenAI endpoints
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/chat/completions", s.handleChatCompletions)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/models", s.handleModels)
	mux.HandleFunc("/v1/models/", s.handleModelDetail)

	// Ollama / Hermes capability probe compatibility
	mux.HandleFunc("/api/tags", s.handleOllamaTags)
	mux.HandleFunc("/api/show", s.handleOllamaShow)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/props", s.handleProps)
	mux.HandleFunc("/v1/props", s.handleProps)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			s.handleHealth(w, r)
			return
		}
		http.NotFound(w, r)
	})

	return loggingMiddleware(mux)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s (%v)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"service": "gemini-proxy",
		"time":    time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	geminiModels := []string{
		"gemini-3.8-flash-high",
		"gemini-3.8-flash-medium",
		"gemini-3.8-flash-low",
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-medium",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-low",
		"gemini-3.6-flash-high",
	}

	var data []ModelCard
	for _, m := range geminiModels {
		data = append(data, ModelCard{
			ID:      m,
			Object:  "model",
			Created: now,
			OwnedBy: "google",
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ModelList{
		Object: "list",
		Data:   data,
	})
}

func (s *Server) handleModelDetail(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	canonical := worker.NormalizeModel(modelID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ModelCard{
		ID:      canonical,
		Object:  "model",
		Created: time.Now().Unix(),
		OwnedBy: "google",
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"version": "1.0.0"})
}

func (s *Server) handleProps(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"default_generation_settings": map[string]any{
			"n_ctx": 1048576,
		},
	})
}

func (s *Server) handleOllamaTags(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"models": []map[string]any{
			{
				"name":        worker.DefaultModel,
				"model":       worker.DefaultModel,
				"modified_at": time.Now().Format(time.RFC3339),
				"size":        0,
			},
		},
	})
}

func (s *Server) handleOllamaShow(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"parameters": "",
		"template":   "",
		"details": map[string]any{
			"format":             "custom",
			"family":             "gemini",
			"parameter_size":     "128B",
			"quantization_level": "none",
		},
		"model_info": map[string]any{
			"general.architecture": "gemini",
		},
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.activeRequests.Add(1)
	if s.onActivity != nil {
		s.onActivity()
	}
	defer func() {
		s.activeRequests.Add(-1)
		s.lastActivity.Store(time.Now().UnixNano())
	}()

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON body: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Messages) == 0 {
		http.Error(w, "messages array must not be empty", http.StatusBadRequest)
		return
	}

	canonicalModel := worker.NormalizeModel(req.Model)
	log.Printf("[Chat] Incoming request for model '%s' (normalized -> '%s'), %d messages, %d tools, stream=%v",
		req.Model, canonicalModel, len(req.Messages), len(req.Tools), req.Stream)

	formattedPrompt := prompt.FormatPrompt(req.Messages, req.Tools)

	wkr, err := s.pool.GetWorker(r.Context(), canonicalModel)
	if err != nil {
		log.Printf("[Chat] Error acquiring worker: %v", err)
		http.Error(w, fmt.Sprintf("failed to get worker for model %s: %v", canonicalModel, err), http.StatusInternalServerError)
		return
	}

	completionID := generateCompletionID()
	created := time.Now().Unix()

	if req.Stream {
		s.handleStreaming(w, r, wkr, canonicalModel, completionID, created, formattedPrompt, len(req.Tools) > 0)
	} else {
		s.handleNonStreaming(w, r, wkr, canonicalModel, completionID, created, formattedPrompt)
	}
}

func (s *Server) handleStreaming(w http.ResponseWriter, r *http.Request, wkr *worker.Worker, model, id string, created int64, formattedPrompt string, hasTools bool) {
	sse, err := NewSSEWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send initial role delta
	_ = sse.SendChunk(ChatCompletionChunk{
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChunkChoice{
			{
				Index: 0,
				Delta: ChunkDelta{Role: "assistant"},
			},
		},
	})

	var mu sync.Mutex
	var rawAccumulator strings.Builder
	hasSeenToolCallTag := false

	result, err := wkr.SendPrompt(r.Context(), formattedPrompt, func(delta string) {
		mu.Lock()
		defer mu.Unlock()

		rawAccumulator.WriteString(delta)
		currentFull := rawAccumulator.String()

		if hasTools && strings.Contains(currentFull, "<tool_call>") {
			// Once a tool call block begins, buffer remaining deltas to parse complete tool call
			hasSeenToolCallTag = true
			return
		}

		if !hasSeenToolCallTag {
			// Stream normal text delta directly
			_ = sse.SendChunk(ChatCompletionChunk{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   model,
				Choices: []ChunkChoice{
					{
						Index: 0,
						Delta: ChunkDelta{Content: delta},
					},
				},
			})
		}
	})

	if err != nil {
		log.Printf("[Chat Stream] Error during turn: %v", err)
		_ = sse.SendDone()
		return
	}

	fullResponse := result.Response
	if fullResponse == "" {
		fullResponse = rawAccumulator.String()
	}

	parsed := prompt.ParseResponse(fullResponse)

	// If tool calls were parsed, send them in the final chunk
	if len(parsed.ToolCalls) > 0 {
		finishReason := parsed.FinishReason
		_ = sse.SendChunk(ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []ChunkChoice{
				{
					Index: 0,
					Delta: ChunkDelta{
						ToolCalls: parsed.ToolCalls,
					},
					FinishReason: &finishReason,
				},
			},
		})
	} else {
		// Normal completion finish reason
		stopReason := "stop"
		_ = sse.SendChunk(ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []ChunkChoice{
				{
					Index:        0,
					Delta:        ChunkDelta{},
					FinishReason: &stopReason,
				},
			},
		})
	}

	_ = sse.SendDone()
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, r *http.Request, wkr *worker.Worker, model, id string, created int64, formattedPrompt string) {
	var fullText strings.Builder
	result, err := wkr.SendPrompt(r.Context(), formattedPrompt, func(delta string) {
		fullText.WriteString(delta)
	})
	if err != nil {
		log.Printf("[Chat] Error executing prompt: %v", err)
		http.Error(w, fmt.Sprintf("execution error: %v", err), http.StatusInternalServerError)
		return
	}

	responseBody := result.Response
	if responseBody == "" {
		responseBody = fullText.String()
	}

	parsed := prompt.ParseResponse(responseBody)

	usage := Usage{}
	if result.Usage != nil {
		usage.PromptTokens = result.Usage.InputTokens
		usage.CompletionTokens = result.Usage.OutputTokens
		usage.TotalTokens = result.Usage.TotalTokens
	}

	resp := ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:      "assistant",
					Content:   parsed.Content,
					ToolCalls: parsed.ToolCalls,
				},
				FinishReason: parsed.FinishReason,
			},
		},
		Usage: usage,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
