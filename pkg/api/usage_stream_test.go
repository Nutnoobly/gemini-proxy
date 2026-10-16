package api

import (
	"context"
	"encoding/json"
	"errors"
	"gemini-proxy/pkg/worker"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type usageWorker struct {
	result *worker.ResultPayload
	err    error
}

func (w usageWorker) SendPrompt(ctx context.Context, text string, delta func(string)) (*worker.ResultPayload, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if w.err == nil && w.result != nil {
		delta(w.result.Response)
	}
	return w.result, w.err
}

func TestStreamIncludesFinalUsageOnlyWhenRequested(t *testing.T) {
	for _, include := range []bool{false, true} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		upstream := usageWorker{result: &worker.ResultPayload{Status: "SUCCESS", Response: "hello", Usage: &worker.UsageStats{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}}}
		s := &Server{}
		s.handleStreaming(w, req, upstream, "model", "id", 1, "prompt", false, include)
		body := w.Body.String()
		if !strings.HasSuffix(body, "data: [DONE]\n\n") {
			t.Fatal(body)
		}
		chunks := strings.Split(strings.TrimSuffix(body, "data: [DONE]\n\n"), "\n\n")
		count := 0
		for _, line := range chunks {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var chunk map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
				t.Fatal(err)
			}
			if usage, ok := chunk["usage"]; ok {
				count++
				choices, ok := chunk["choices"].([]any)
				if !ok || len(choices) != 0 {
					t.Fatalf("usage choices must be []: %s", line)
				}
				if usage.(map[string]any)["total_tokens"] != float64(15) {
					t.Fatal(usage)
				}
			}
		}
		expected := 0
		if include {
			expected = 1
		}
		if count != expected {
			t.Fatalf("include=%v usage chunks=%d body=%s", include, count, body)
		}
		if include && !strings.Contains(chunks[len(chunks)-2], `"usage"`) {
			t.Fatalf("usage must be last: %s", body)
		}
	}
}
func TestNonStreamingUnknownMetadataRemainsUnavailable(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	s := &Server{}
	s.handleNonStreaming(w, req, usageWorker{result: &worker.ResultPayload{Status: "SUCCESS", Response: "hello"}}, "model", "id", 1, "prompt")
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["usage"]; ok {
		t.Fatalf("unknown usage fabricated: %s", w.Body.String())
	}
}

type brokenStream struct{ header http.Header }

func (w *brokenStream) Header() http.Header     { return w.header }
func (*brokenStream) WriteHeader(int)           {}
func (*brokenStream) Flush()                    {}
func (*brokenStream) Write([]byte) (int, error) { return 0, errors.New("client disconnected") }

type countingWorker struct{ calls int }

func (w *countingWorker) SendPrompt(context.Context, string, func(string)) (*worker.ResultPayload, error) {
	w.calls++
	return &worker.ResultPayload{Response: "hello"}, nil
}
func TestBrokenStreamDoesNotStartUpstream(t *testing.T) {
	w := &brokenStream{header: make(http.Header)}
	upstream := &countingWorker{}
	s := &Server{}
	s.handleStreaming(w, httptest.NewRequest("POST", "/v1/chat/completions", nil), upstream, "model", "id", 1, "prompt", false, true)
	if upstream.calls != 0 {
		t.Fatalf("started upstream after initial write failed: %d", upstream.calls)
	}
}

type disconnectAfterRole struct {
	brokenStream
	writes int
}

func (w *disconnectAfterRole) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > 1 {
		return 0, errors.New("disconnected")
	}
	return len(data), nil
}

type cancellationWorker struct{ canceled bool }

func (w *cancellationWorker) SendPrompt(ctx context.Context, text string, delta func(string)) (*worker.ResultPayload, error) {
	delta("hello")
	w.canceled = ctx.Err() != nil
	if w.canceled {
		return nil, ctx.Err()
	}
	return &worker.ResultPayload{Response: "hello"}, nil
}
func TestDisconnectCancelsUpstream(t *testing.T) {
	w := &disconnectAfterRole{brokenStream: brokenStream{header: make(http.Header)}}
	upstream := &cancellationWorker{}
	s := &Server{}
	s.handleStreaming(w, httptest.NewRequest("POST", "/v1/chat/completions", nil), upstream, "model", "id", 1, "prompt", false, true)
	if !upstream.canceled {
		t.Fatal("client failure did not cancel upstream")
	}
}
func TestServerCloseReleasesTracker(t *testing.T) {
	s := NewServer(nil, WithUsageFile(""))
	s.tracker.Record("m", nil)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s.tracker.Record("m", nil)
	if s.tracker.Stats().CompletedRequests != 1 {
		t.Fatal("server tracker still active")
	}
}
func TestResponseUsageParityAndExactlyOnceRecording(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, known := range []bool{false, true} {
			tracker := NewUsageTracker(WithUsageFile(""))
			defer tracker.Close()
			s := &Server{tracker: tracker}
			var raw *worker.UsageStats
			if known {
				raw = &worker.UsageStats{InputTokens: 10, OutputTokens: 5, ThinkingTokens: 2, CacheReadTokens: 20, TotalTokens: 15}
			}
			upstream := usageWorker{result: &worker.ResultPayload{Status: "SUCCESS", Response: `<tool_call>{"name":"read","arguments":{}}</tool_call>`, Usage: raw}}
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			if stream {
				s.handleStreaming(w, req, upstream, "model", "id", 1, "prompt", true, true)
			} else {
				s.handleNonStreaming(w, req, upstream, "model", "id", 1, "prompt")
			}
			got := tracker.Stats()
			if got.CompletedRequests != 1 {
				t.Fatalf("completion counted %d times", got.CompletedRequests)
			}
			if known && (got.MeasuredRequests != 1 || got.PromptTokens != 30 || got.CompletionTokens != 5 || got.TotalTokens != 35 || got.ReasoningTokens != 2 || got.CachedTokens != 20) {
				t.Fatalf("wrong totals: %+v", got)
			}
			if !known && (got.UnknownUsageRequests != 1 || strings.Contains(w.Body.String(), `"usage"`)) {
				t.Fatalf("unknown usage: %+v %s", got, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), `"finish_reason":"tool_calls"`) {
				t.Fatalf("tool completion changed: %s", w.Body.String())
			}
			if known && !strings.Contains(w.Body.String(), `"cached_tokens":20`) {
				t.Fatalf("cached detail missing: %s", w.Body.String())
			}
		}
	}
}
func TestFailedAndCanceledRequestsNotRecorded(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			tracker := NewUsageTracker(WithUsageFile(""))
			defer tracker.Close()
			s := &Server{tracker: tracker}
			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			if canceled {
				cancel()
			}
			req = req.WithContext(ctx)
			upstream := usageWorker{err: errors.New("upstream failed")}
			w := httptest.NewRecorder()
			if stream {
				s.handleStreaming(w, req, upstream, "model", "id", 1, "prompt", false, true)
			} else {
				s.handleNonStreaming(w, req, upstream, "model", "id", 1, "prompt")
			}
			if tracker.Stats().CompletedRequests != 0 {
				t.Fatal("failed request counted as complete")
			}
			if strings.Contains(w.Body.String(), `"usage"`) {
				t.Fatal("failed request fabricated usage")
			}
			if stream && canceled && strings.Contains(w.Body.String(), "[DONE]") {
				t.Fatal("canceled stream must not write terminal data")
			}
		}
	}
}
