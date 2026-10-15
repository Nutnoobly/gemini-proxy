package worker

import "testing"

func TestNormalizeModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Defaults and fallbacks
		{"", "gemini-3.8-flash-high"},
		{"default", "gemini-3.8-flash-high"},
		{"gemini", "gemini-3.8-flash-high"},

		// Gemini 4 Argon variants
		{"gemini-4-argon", "gemini-4-argon-high"},
		{"gemini-4-argon-high", "gemini-4-argon-high"},
		{"gemini4-argon", "gemini-4-argon-high"},
		{"gemini4 argon", "gemini-4-argon-high"},
		{"gemini 4 argon", "gemini-4-argon-high"},
		{"argon", "gemini-4-argon-high"},
		{"argon-high", "gemini-4-argon-high"},
		{"gemini-4-high", "gemini-4-argon-high"},
		{"gemini4-high", "gemini-4-argon-high"},
		{"gemini-argon", "gemini-4-argon-high"},
		{"google/gemini-4-argon", "gemini-4-argon-high"},
		{"custom/gemini-4-argon-high", "gemini-4-argon-high"},

		{"gemini-4-argon-medium", "gemini-4-argon-medium"},
		{"gemini4-argon-medium", "gemini-4-argon-medium"},
		{"gemini 4 argon medium", "gemini-4-argon-medium"},
		{"argon-medium", "gemini-4-argon-medium"},
		{"argon med", "gemini-4-argon-medium"},

		{"gemini-4-argon-low", "gemini-4-argon-low"},
		{"gemini4-argon-low", "gemini-4-argon-low"},
		{"gemini 4 argon low", "gemini-4-argon-low"},
		{"argon-low", "gemini-4-argon-low"},

		// Existing models
		{"gemini-3.8-flash-high", "gemini-3.8-flash-high"},
		{"flash", "gemini-3.8-flash-high"},
		{"gemini-3.8-flash-medium", "gemini-3.8-flash-medium"},
		{"gemini-3.8-flash-low", "gemini-3.8-flash-low"},
		{"gemini-3.1-pro-high", "gemini-3.1-pro-high"},
		{"pro", "gemini-3.1-pro-high"},
		{"gemini-3.7-flash-high", "gemini-3.7-flash-high"},
	}

	for _, tc := range tests {
		actual := NormalizeModel(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizeModel(%q) = %q; expected %q", tc.input, actual, tc.expected)
		}
	}
}
