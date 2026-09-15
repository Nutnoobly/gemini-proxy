package worker

import "encoding/json"

// StreamInputMessage is the NDJSON message sent to agy's stdin.
type StreamInputMessage struct {
	Event   string                 `json:"event"`
	Message StreamInputUserMessage `json:"message"`
}

// StreamInputUserMessage holds the content sent to agy.
type StreamInputUserMessage struct {
	Content string `json:"content"`
}

// StreamOutputEvent is the base envelope received from agy's stdout.
type StreamOutputEvent struct {
	Event          string          `json:"event"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Init           *InitPayload    `json:"init,omitempty"`
	StepUpdate     *StepPayload    `json:"step_update,omitempty"`
	Result         *ResultPayload  `json:"result,omitempty"`
	Raw            json.RawMessage `json:"-"`
}

// InitPayload is received upon agy startup.
type InitPayload struct {
	Model          string   `json:"model"`
	CWD            string   `json:"cwd"`
	Tools          []string `json:"tools"`
	PermissionMode string   `json:"permission_mode"`
}

// UsageStats records token consumption.
type UsageStats struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ThinkingTokens  int `json:"thinking_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

// StepPayload represents a turn progression event.
type StepPayload struct {
	ConversationID  string      `json:"conversation_id"`
	StepIndex       int         `json:"step_index"`
	State           string      `json:"state"` // "ACTIVE", "DONE"
	StepType        string      `json:"step_type"` // "agent_response", "user_input"
	TextDelta       string      `json:"text_delta"`
	DurationSeconds float64     `json:"duration_seconds"`
	Usage           *UsageStats `json:"usage,omitempty"`
}

// ResultPayload is the terminal event for a turn or session.
type ResultPayload struct {
	ConversationID  string      `json:"conversation_id"`
	Status          string      `json:"status"` // "SUCCESS", "ERROR"
	Response        string      `json:"response"`
	Error           string      `json:"error,omitempty"`
	DurationSeconds float64     `json:"duration_seconds"`
	NumTurns        int         `json:"num_turns"`
	Usage           *UsageStats `json:"usage,omitempty"`
}
