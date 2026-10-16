package api

import "gemini-proxy/pkg/worker"

// normalizeUsage maps agy 1.3.6 per-request deltas to OpenAI accounting.
// agy input and total exclude cache reads; output already includes thinking.
// See docs/usage-accounting.md for official examples and sanitized live evidence.
func normalizeUsage(raw *worker.UsageStats) *Usage {
	if !raw.Valid() {
		return nil
	}
	max := int(^uint(0) >> 1)
	if raw.CacheReadTokens > max-raw.TotalTokens {
		return nil
	}
	prompt := raw.InputTokens + raw.CacheReadTokens
	return &Usage{
		PromptTokens: prompt, CompletionTokens: raw.OutputTokens, TotalTokens: raw.TotalTokens + raw.CacheReadTokens,
		PromptTokensDetails:     &PromptTokensDetails{CachedTokens: raw.CacheReadTokens},
		CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: raw.ThinkingTokens},
	}
}

func (s *Server) recordUsage(model string, raw *worker.UsageStats) *Usage {
	usage := normalizeUsage(raw)
	if s.tracker != nil {
		s.tracker.Record(model, usage)
	}
	return usage
}
