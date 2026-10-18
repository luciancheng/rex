package watch

import (
	"encoding/json"
	"fmt"
	"strings"
)

var cursorEvents = map[string]bool{
	"afterAgentThought":    true,
	"beforeSubmitPrompt":   true,
	"beforeShellExecution": true,
	"preToolUse":           true,
	"afterAgentResponse":   true,
	"stop":                 true,
}

var claudeEvents = map[string]bool{
	"PreToolUse":       true,
	"PostToolUse":      true,
	"UserPromptSubmit": true,
	"Notification":     true,
	"Stop":             true,
	"SessionStart":     true,
}

var codexTypes = map[string]bool{
	"thread.started": true,
	"turn.started":   true,
	"turn.completed": true,
	"turn.failed":    true,
	"item.started":   true,
	"item.updated":   true,
	"item.completed": true,
	"error":          true,
}

var skipToolKeys = map[string]bool{
	"content":    true,
	"contents":   true,
	"old_string": true,
	"new_string": true,
	"new_text":   true,
	"diff":       true,
}

func asDict(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return v
	case string:
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) != nil {
			return map[string]any{}
		}
		if m, ok := parsed.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func asCommand(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			parts = append(parts, fmt.Sprint(part))
		}
		return strings.Join(parts, " ")
	case []string:
		return strings.Join(v, " ")
	case map[string]any:
		if cmd, ok := v["command"]; ok {
			return asCommand(cmd)
		}
		if cmd, ok := v["cmd"]; ok {
			return asCommand(cmd)
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		return fmt.Sprint(v)
	}
}

func hookName(payload map[string]any) string {
	for _, key := range []string{"hook_event_name", "hookEventName", "event"} {
		if v, ok := payload[key]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func DetectSource(payload map[string]any, hint string) string {
	if hint != "" && hint != "auto" {
		return hint
	}
	name := hookName(payload)
	if claudeEvents[name] {
		return "claude"
	}
	if cursorEvents[name] {
		return "cursor"
	}
	if _, ok := payload["conversation_id"]; ok {
		return "cursor"
	}
	if _, ok := payload["generation_id"]; ok {
		return "cursor"
	}
	typ := fmt.Sprint(payload["type"])
	if codexTypes[typ] {
		return "codex"
	}
	if _, ok := payload["item"].(map[string]any); ok {
		return "codex"
	}
	if name != "" {
		return "cursor"
	}
	return "cursor"
}

func toolKind(tool string) string {
	lowered := strings.ToLower(tool)
	if strings.Contains(lowered, "ask") || strings.Contains(lowered, "question") {
		return "question"
	}
	return "command"
}

func strField(data map[string]any, key string) string {
	v, ok := data[key]
	if !ok || v == nil {
		return ""
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

func toolText(tool string, rawInput any) string {
	var data map[string]any
	if _, isList := rawInput.([]any); !isList {
		data = asDict(rawInput)
	} else {
		data = map[string]any{}
	}
	if q := strField(data, "question"); q != "" {
		return q
	}
	if p := strField(data, "prompt"); p != "" {
		return p
	}
	if _, ok := data["command"]; ok {
		return asCommand(data)
	}
	if _, ok := data["cmd"]; ok {
		return asCommand(data)
	}
	if p := strField(data, "file_path"); p != "" {
		return tool + " " + p
	}
	if p := strField(data, "path"); p != "" {
		return tool + " " + p
	}
	if q := strField(data, "query"); q != "" {
		return tool + " " + q
	}
	for key := range skipToolKeys {
		if _, ok := data[key]; ok {
			leftover := map[string]any{}
			for k, v := range data {
				if !skipToolKeys[k] {
					leftover[k] = v
				}
			}
			if len(leftover) > 0 {
				b, _ := json.Marshal(leftover)
				return tool + " " + string(b)
			}
			return tool
		}
	}
	command := asCommand(rawInput)
	if command != "" && command != "{}" {
		return command
	}
	if len(data) > 0 {
		b, _ := json.Marshal(data)
		return tool + " " + string(b)
	}
	return tool
}

func MapCursor(payload map[string]any) *Event {
	name := hookName(payload)
	switch name {
	case "beforeSubmitPrompt":
		text := strings.TrimSpace(strField(payload, "prompt"))
		if text == "" {
			text = strings.TrimSpace(strField(payload, "text"))
		}
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", "user", text, nil)
		return &ev
	case "afterAgentThought":
		text := strings.TrimSpace(strField(payload, "text"))
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", "thought", text, nil)
		return &ev
	case "beforeShellExecution":
		command := strings.TrimSpace(asCommand(payload["command"]))
		if command == "" {
			command = strings.TrimSpace(asCommand(payload["tool_input"]))
		}
		if command == "" {
			return nil
		}
		ev := NewEvent("cursor", "command", command, map[string]any{"tool": "shell"})
		return &ev
	case "preToolUse":
		tool := strField(payload, "tool_name")
		if tool == "" {
			tool = strField(payload, "toolName")
		}
		if tool == "" {
			tool = "tool"
		}
		input := payload["tool_input"]
		if input == nil {
			input = payload["toolInput"]
		}
		text := strings.TrimSpace(toolText(tool, input))
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", toolKind(tool), text, map[string]any{"tool": tool})
		return &ev
	case "afterAgentResponse":
		text := strings.TrimSpace(strField(payload, "text"))
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", "text", text, nil)
		return &ev
	}
	return nil
}

func MapClaude(payload map[string]any) *Event {
	name := hookName(payload)
	switch name {
	case "PreToolUse":
		tool := strField(payload, "tool_name")
		if tool == "" {
			tool = strField(payload, "toolName")
		}
		if tool == "" {
			tool = "tool"
		}
		input := payload["tool_input"]
		if input == nil {
			input = payload["toolInput"]
		}
		text := strings.TrimSpace(toolText(tool, input))
		if text == "" {
			return nil
		}
		ev := NewEvent("claude", toolKind(tool), text, map[string]any{"tool": tool})
		return &ev
	case "PostToolUse":
		tool := strField(payload, "tool_name")
		if tool == "" {
			tool = strField(payload, "toolName")
		}
		if tool == "" {
			tool = "tool"
		}
		ev := NewEvent("claude", "result", tool, map[string]any{"tool": tool})
		return &ev
	case "Notification":
		text := strings.TrimSpace(strField(payload, "message"))
		if text == "" {
			text = strings.TrimSpace(strField(payload, "text"))
		}
		if text == "" {
			return nil
		}
		ev := NewEvent("claude", "question", text, nil)
		return &ev
	case "UserPromptSubmit":
		text := strings.TrimSpace(strField(payload, "prompt"))
		if text == "" {
			text = strings.TrimSpace(strField(payload, "text"))
		}
		if text == "" {
			return nil
		}
		ev := NewEvent("claude", "user", text, nil)
		return &ev
	}
	return nil
}

func MapCodex(payload map[string]any) *Event {
	item, ok := payload["item"].(map[string]any)
	if !ok {
		return nil
	}
	itemType := strField(item, "type")
	var text, kind string
	switch itemType {
	case "reasoning":
		text = strings.TrimSpace(strField(item, "text"))
		if text == "" {
			text = strings.TrimSpace(strField(item, "content"))
		}
		kind = "thought"
	case "agent_message", "agentMessage":
		text = strings.TrimSpace(strField(item, "text"))
		kind = "text"
	case "command_execution", "commandExecution":
		text = strings.TrimSpace(asCommand(item["command"]))
		kind = "command"
	default:
		return nil
	}
	if text == "" {
		return nil
	}
	ev := NewEvent("codex", kind, text, map[string]any{"item": itemType})
	return &ev
}

func MapPayload(payload map[string]any, source string) *Event {
	resolved := DetectSource(payload, source)
	switch resolved {
	case "claude":
		return MapClaude(payload)
	case "codex":
		return MapCodex(payload)
	default:
		return MapCursor(payload)
	}
}
