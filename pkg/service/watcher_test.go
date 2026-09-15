package service

import (
	"testing"
)

func TestIsHermesCmdline(t *testing.T) {
	cases := []struct {
		name     string
		cmdline  string
		expected bool
	}{
		{
			name:     "Direct hermes binary",
			cmdline:  "/home/nutnoobly/.local/bin/hermes\x00chat\x00-q\x00hello\x00",
			expected: true,
		},
		{
			name:     "Python hermes_cli module",
			cmdline:  "/usr/bin/python3\x00-m\x00hermes_cli.main\x00gateway\x00run\x00",
			expected: true,
		},
		{
			name:     "Node hermes-agent TUI",
			cmdline:  "/usr/bin/node\x00/home/nutnoobly/.hermes/hermes-agent/ui-tui/dist/entry.js\x00",
			expected: true,
		},
		{
			name:     "Python hermes-agent script",
			cmdline:  "/home/nutnoobly/.hermes/hermes-agent/venv/bin/python\x00/home/nutnoobly/.hermes/hermes-agent/hermes\x00dashboard\x00",
			expected: true,
		},
		{
			name:     "Self gemini-proxy process",
			cmdline:  "/home/nutnoobly/User/Code/GeminiProxy/gemini-proxy\x00serve\x00",
			expected: false,
		},
		{
			name:     "Text editor opening hermes file",
			cmdline:  "/usr/bin/nano\x00/tmp/hermes.txt\x00",
			expected: false,
		},
		{
			name:     "Grep search for hermes",
			cmdline:  "grep\x00-i\x00hermes\x00",
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			actual := isHermesCmdline(tc.cmdline)
			if actual != tc.expected {
				t.Fatalf("expected %v but got %v for cmdline %q", tc.expected, actual, tc.cmdline)
			}
		})
	}
}
