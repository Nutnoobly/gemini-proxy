package prompt

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"gemini-proxy/pkg/types"
)

var toolCallRegex = regexp.MustCompile(`(?s)<tool_call>(.*?)(?:</tool_call>|$)`)

// ParsedOutput contains the cleaned content and any detected tool calls.
type ParsedOutput struct {
	Content      string
	ToolCalls    []types.ToolCall
	FinishReason string
}

// GenerateToolCallID generates a random unique ID for a tool call.
func GenerateToolCallID() string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return "call_" + hex.EncodeToString(bytes)
}

// ParseResponse extracts text content and tool calls from the raw model response.
func ParseResponse(raw string) ParsedOutput {
	matches := toolCallRegex.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return ParsedOutput{
			Content:      strings.TrimSpace(raw),
			ToolCalls:    nil,
			FinishReason: "stop",
		}
	}

	var toolCalls []types.ToolCall
	var textParts []string
	lastIdx := 0

	for i, match := range matches {
		fullStart, fullEnd := match[0], match[1]
		bodyStart, bodyEnd := match[2], match[3]

		// Text before this tool call
		if fullStart > lastIdx {
			chunk := strings.TrimSpace(raw[lastIdx:fullStart])
			if chunk != "" {
				textParts = append(textParts, chunk)
			}
		}
		lastIdx = fullEnd

		body := strings.TrimSpace(raw[bodyStart:bodyEnd])
		// Strip possible code block markers (e.g. ```json ... ```)
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)

		var parsedCall struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}

		if err := json.Unmarshal([]byte(body), &parsedCall); err == nil && parsedCall.Name != "" {
			argStr := string(parsedCall.Arguments)
			if argStr == "" || argStr == "null" {
				argStr = "{}"
			}

			idx := i
			toolCalls = append(toolCalls, types.ToolCall{
				Index: &idx,
				ID:    GenerateToolCallID(),
				Type:  "function",
				Function: types.FunctionCall{
					Name:      parsedCall.Name,
					Arguments: argStr,
				},
			})
		}
	}

	// Remaining text after last tool call
	if lastIdx < len(raw) {
		chunk := strings.TrimSpace(raw[lastIdx:])
		if chunk != "" {
			textParts = append(textParts, chunk)
		}
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	return ParsedOutput{
		Content:      strings.Join(textParts, "\n\n"),
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}
}
