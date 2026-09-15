package prompt

import (
	"encoding/json"
	"fmt"
	"strings"

	"gemini-proxy/pkg/types"
)

// FormatPrompt constructs a structured prompt from OpenAI messages and tool declarations.
func FormatPrompt(messages []types.ChatMessage, tools []types.Tool) string {
	var sb strings.Builder

	// Prepend completion engine notice so model does not run CLI commands
	sb.WriteString("[SYSTEM NOTICE: You are acting strictly as an LLM completion backend. DO NOT execute host commands or call local tools directly. Only return text or requested <tool_call> XML tags.]\n\n")

	// If tools are provided, prepend system tool guidance
	if len(tools) > 0 {
		sb.WriteString("[SYSTEM INSTRUCTION: TOOL CALLING]\n")
		sb.WriteString("You are an AI assistant acting as the core intelligence engine for an autonomous agent.\n")
		sb.WriteString("You have access to the following tools:\n\n")

		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("### Tool: %s\n", t.Function.Name))
			if t.Function.Description != "" {
				sb.WriteString(fmt.Sprintf("Description: %s\n", t.Function.Description))
			}
			if len(t.Function.Parameters) > 0 {
				sb.WriteString(fmt.Sprintf("Parameters JSON Schema:\n%s\n", string(t.Function.Parameters)))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("TOOL CALLING RULES:\n")
		sb.WriteString("1. When you need to call a tool, you MUST emit a tool call block using exact XML tags:\n")
		sb.WriteString("<tool_call>\n")
		sb.WriteString("{\"name\": \"<tool_name>\", \"arguments\": {<arguments_json_object>}}\n")
		sb.WriteString("</tool_call>\n")
		sb.WriteString("2. You may emit multiple <tool_call>...</tool_call> blocks if multiple tools need to be called in parallel.\n")
		sb.WriteString("3. You may include concise explanation before or after the tool calls if helpful, or emit only the tool calls.\n")
		sb.WriteString("4. Do NOT attempt to run local tools yourself; only output the <tool_call> blocks for the caller to execute.\n")
		sb.WriteString("5. If no tool call is required, respond normally with text.\n")
		sb.WriteString("[END SYSTEM INSTRUCTION]\n\n")
	}

	// Format conversation history
	for _, msg := range messages {
		role := strings.ToLower(msg.Role)
		switch role {
		case "system":
			sb.WriteString("System: ")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		case "user":
			sb.WriteString("User: ")
			sb.WriteString(msg.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("Assistant: ")
			if msg.Content != "" {
				sb.WriteString(msg.Content)
				sb.WriteString("\n")
			}
			for _, tc := range msg.ToolCalls {
				sb.WriteString("<tool_call>\n")
				callData := map[string]any{
					"name":      tc.Function.Name,
					"arguments": json.RawMessage(tc.Function.Arguments),
				}
				data, _ := json.Marshal(callData)
				sb.WriteString(string(data))
				sb.WriteString("\n</tool_call>\n")
			}
			sb.WriteString("\n")
		case "tool", "function":
			toolIdentifier := msg.Name
			if toolIdentifier == "" {
				toolIdentifier = msg.ToolCallID
			}
			sb.WriteString(fmt.Sprintf("Tool Result [%s]:\n%s\n\n", toolIdentifier, msg.Content))
		default:
			sb.WriteString(fmt.Sprintf("%s: %s\n\n", strings.Title(role), msg.Content))
		}
	}

	sb.WriteString("Assistant: ")
	return sb.String()
}
