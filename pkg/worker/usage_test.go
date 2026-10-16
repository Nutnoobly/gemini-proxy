package worker

import (
	"encoding/json"
	"testing"
)

func TestUsageDeltaExcludesPreviousTurns(t *testing.T) {
	w := &Worker{}
	first := &UsageStats{InputTokens: 16813, OutputTokens: 1, TotalTokens: 16814}
	if got := w.turnUsage(first); got == nil || *got != *first {
		t.Fatalf("first usage=%+v", got)
	}
	second := &UsageStats{InputTokens: 18940, OutputTokens: 2, CacheReadTokens: 14763, TotalTokens: 18942}
	got := w.turnUsage(second)
	if got == nil || got.InputTokens != 2127 || got.OutputTokens != 1 || got.CacheReadTokens != 14763 || got.TotalTokens != 2128 {
		t.Fatalf("cumulative usage double counted: %+v", got)
	}
	if first.InputTokens != 16813 || second.InputTokens != 18940 {
		t.Fatal("mutated upstream payload")
	}
}
func TestUsageDeltaMissingBaselineAndRegression(t *testing.T) {
	w := &Worker{}
	if got := w.turnUsage(nil); got != nil {
		t.Fatal(got)
	}
	if got := w.turnUsage(&UsageStats{InputTokens: 5, TotalTokens: 5}); got != nil {
		t.Fatalf("unknown interval attributed to one request: %+v", got)
	}
	if got := w.turnUsage(&UsageStats{InputTokens: 8, TotalTokens: 8}); got == nil || got.InputTokens != 3 {
		t.Fatalf("baseline not recovered: %+v", got)
	}
	if got := w.turnUsage(&UsageStats{InputTokens: 1, TotalTokens: 1}); got != nil {
		t.Fatalf("regression reported: %+v", got)
	}
}
func TestUsageMetadataMustContainAllCounters(t *testing.T) {
	for _, text := range []string{`{}`, `{"input_tokens":null,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}`, `{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0}`} {
		var raw UsageStats
		if err := json.Unmarshal([]byte(text), &raw); err != nil {
			t.Fatal(err)
		}
		if raw.Valid() {
			t.Fatalf("incomplete metadata accepted: %s", text)
		}
	}
	var raw UsageStats
	if err := json.Unmarshal([]byte(`{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0}`), &raw); err != nil {
		t.Fatal(err)
	}
	if !raw.Valid() {
		t.Fatal("explicit zeros rejected")
	}
}
func TestMalformedUsageDoesNotLoseCompletedResponse(t *testing.T) {
	var event StreamOutputEvent
	text := `{"event":"result","result":{"status":"SUCCESS","response":"hello","usage":{"input_tokens":"invalid","output_tokens":1,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":1}}}`
	if err := json.Unmarshal([]byte(text), &event); err != nil {
		t.Fatalf("malformed usage discarded entire response: %v", err)
	}
	if event.Result.Response != "hello" || event.Result.Usage.Valid() {
		t.Fatal("invalid metadata accepted or response lost")
	}
}
