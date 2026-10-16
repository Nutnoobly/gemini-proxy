package worker

import "encoding/json"

// turnUsage converts agy's conversation-cumulative terminal counters into a
// request delta. SendPrompt holds turnMu; step_update totals are never added.
// A missing interval cannot safely be attributed to a later request.
func (w *Worker) turnUsage(current *UsageStats) *UsageStats {
	if current == nil || !current.Valid() {
		w.usageGap = true
		return nil
	}
	previous := w.lastUsage
	gap := w.usageGap
	w.lastUsage = *current
	w.usageGap = false
	delta := &UsageStats{
		InputTokens:     current.InputTokens - previous.InputTokens,
		OutputTokens:    current.OutputTokens - previous.OutputTokens,
		ThinkingTokens:  current.ThinkingTokens - previous.ThinkingTokens,
		CacheReadTokens: current.CacheReadTokens - previous.CacheReadTokens,
		TotalTokens:     current.TotalTokens - previous.TotalTokens,
	}
	if gap || delta.InputTokens < 0 || delta.OutputTokens < 0 || delta.ThinkingTokens < 0 || delta.CacheReadTokens < 0 || delta.TotalTokens < 0 {
		return nil
	}
	return delta
}

// Valid distinguishes complete measured metadata from missing/invalid counters.
func (u *UsageStats) Valid() bool {
	return u != nil && !u.incomplete && u.InputTokens >= 0 && u.OutputTokens >= 0 && u.ThinkingTokens >= 0 && u.CacheReadTokens >= 0 && u.TotalTokens >= 0 && u.InputTokens <= u.TotalTokens && u.OutputTokens == u.TotalTokens-u.InputTokens && u.ThinkingTokens <= u.OutputTokens
}

func (u *UsageStats) UnmarshalJSON(data []byte) error {
	*u = UsageStats{}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		u.incomplete = true
		return nil
	}
	values := map[string]*int{"input_tokens": &u.InputTokens, "output_tokens": &u.OutputTokens, "thinking_tokens": &u.ThinkingTokens, "cache_read_tokens": &u.CacheReadTokens, "total_tokens": &u.TotalTokens}
	for key, target := range values {
		var value *int
		if err := json.Unmarshal(fields[key], &value); err != nil || value == nil {
			u.incomplete = true
		} else {
			*target = *value
		}
	}
	return nil
}
