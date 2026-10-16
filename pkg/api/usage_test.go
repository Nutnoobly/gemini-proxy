package api

import (
	"gemini-proxy/pkg/worker"
	"testing"
)

func TestNormalizeUsageVerifiedAgy136(t *testing.T) {
	// Sanitized live agy 1.3.6 second-turn step and terminal delta.
	got := normalizeUsage(&worker.UsageStats{InputTokens: 2167, OutputTokens: 26, ThinkingTokens: 25, CacheReadTokens: 14756, TotalTokens: 2193})
	if got == nil || got.PromptTokens != 16923 || got.CompletionTokens != 26 || got.TotalTokens != 16949 || got.PromptTokensDetails.CachedTokens != 14756 || got.CompletionTokensDetails.ReasoningTokens != 25 {
		t.Fatalf("wrong mapping: %+v", got)
	}
}
func TestNormalizeUsageMissingZeroAndInvalid(t *testing.T) {
	if normalizeUsage(nil) != nil {
		t.Fatal("missing metadata is measured")
	}
	if got := normalizeUsage(&worker.UsageStats{}); got == nil || got.TotalTokens != 0 {
		t.Fatal("measured zeros lost")
	}
	for _, raw := range []*worker.UsageStats{
		{InputTokens: -1}, {InputTokens: 10, OutputTokens: 5, TotalTokens: 20},
		{OutputTokens: 1, ThinkingTokens: 2, TotalTokens: 1}, {CacheReadTokens: -1},
		{InputTokens: int(^uint(0) >> 1), CacheReadTokens: 1, TotalTokens: int(^uint(0) >> 1)},
	} {
		if got := normalizeUsage(raw); got != nil {
			t.Fatalf("invalid usage reported: %+v", got)
		}
	}
}
