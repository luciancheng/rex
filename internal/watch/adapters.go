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
	"postToolUse":          true,
	"postToolUseFailure":   true,
	"afterFileEdit":        true,
	"afterAgentResponse":   true,
	"AgentResponse":        true,
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

func IsFileEditTool(tool string) bool {
	t := strings.ToLower(strings.TrimSpace(tool))
	if t == "" || strings.Contains(t, "read") {
		return false
	}
	for _, needle := range []string{
		"write", "strreplace", "search_replace", "searchreplace",
		"delete", "edit", "applypatch", "apply_patch", "notebook", "multiedit",
	} {
		if strings.Contains(t, needle) {
			return true
		}
	}
	return false
}

func payloadToolName(payload map[string]any) string {
	tool := strField(payload, "tool_name")
	if tool == "" {
		tool = strField(payload, "toolName")
	}
	if tool == "" {
		tool = strField(payload, "tool")
	}
	if tool == "" {
		tool = "tool"
	}
	return tool
}

func payloadPath(payload map[string]any) string {
	for _, key := range []string{"file_path", "filePath", "path"} {
		if p := strField(payload, key); p != "" {
			return p
		}
	}
	input := payload["tool_input"]
	if input == nil {
		input = payload["toolInput"]
	}
	data := asDict(input)
	for _, key := range []string{"file_path", "filePath", "path"} {
		if p := strField(data, key); p != "" {
			return p
		}
	}
	return ""
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

func extractProse(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := extractProse(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		if s := strings.TrimSpace(strField(v, "text")); s != "" {
			return s
		}
		if inner, ok := v["content"]; ok {
			if s := extractProse(inner); s != "" {
				return s
			}
		}
		if s := strings.TrimSpace(strField(v, "message")); s != "" {
			return s
		}
	}
	return ""
}

func assistantText(payload map[string]any) string {
	for _, key := range []string{"text", "content", "message", "response"} {
		if v, ok := payload[key]; ok {
			if s := extractProse(v); s != "" {
				return s
			}
		}
	}
	return ""
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
		text := assistantText(payload)
		if text == "" {
			text = strings.TrimSpace(strField(payload, "text"))
		}
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
		ev := NewEvent("cursor", "command", command, toolMeta("shell", ""))
		return &ev
	case "preToolUse":
		tool := payloadToolName(payload)
		input := payload["tool_input"]
		if input == nil {
			input = payload["toolInput"]
		}
		text := strings.TrimSpace(toolText(tool, input))
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", toolKind(tool), text, toolMeta(tool, payloadPath(payload)))
		return &ev
	case "postToolUse", "postToolUseFailure", "afterFileEdit":
		tool := payloadToolName(payload)
		if tool == "tool" && name == "afterFileEdit" {
			tool = "Write"
		}
		path := payloadPath(payload)
		text := tool
		if path != "" {
			lower := strings.ToLower(tool)
			switch {
			case strings.Contains(lower, "delete"):
				text = "deleted " + path
			case name == "afterFileEdit" || IsFileEditTool(tool):
				text = "wrote " + path
			default:
				text = tool + " " + path
			}
		}
		if name == "postToolUseFailure" {
			ev := NewEvent("cursor", "error", text, toolMeta(tool, path))
			return &ev
		}
		ev := NewEvent("cursor", "result", strings.TrimSpace(text), toolMeta(tool, path))
		return &ev
	case "afterAgentResponse", "AgentResponse":
		text := assistantText(payload)
		if text == "" {
			return nil
		}
		ev := NewEvent("cursor", "text", text, nil)
		return &ev
	case "stop":
		// End-of-turn status only. Mapping it as text hid earlier replies
		// when Cursor sent the full concatenated message.
		return nil
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
		ev := NewEvent("claude", toolKind(tool), text, toolMeta(tool, payloadPath(payload)))
		return &ev
	case "PostToolUse":
		tool := strField(payload, "tool_name")
		if tool == "" {
			tool = strField(payload, "toolName")
		}
		if tool == "" {
			tool = "tool"
		}
		path := payloadPath(payload)
		ev := NewEvent("claude", "result", tool, toolMeta(tool, path))
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
	meta := map[string]any{"item": itemType}
	if kind == "command" {
		meta["tool"] = "shell"
	}
	ev := NewEvent("codex", kind, text, meta)
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
