package prompt

import "testing"

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Errorf("expected 0 tokens for empty string, got %d", EstimateTokens(""))
	}
	if tokens := EstimateTokens("Hello world"); tokens < 1 {
		t.Errorf("expected >= 1 tokens, got %d", tokens)
	}
	sample := "The quick brown fox jumps over the lazy dog"
	tokens := EstimateTokens(sample)
	if tokens < 8 || tokens > 15 {
		t.Errorf("expected token count around 11, got %d", tokens)
	}
}
