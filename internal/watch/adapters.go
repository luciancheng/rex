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
	"PreToolUse":         true,
	"PostToolUse":        true,
	"PostToolUseFailure": true,
	"UserPromptSubmit":   true,
	"Notification":       true,
	"Stop":               true,
	"StopFailure":        true,
	"SessionStart":       true,
	"SessionEnd":         true,
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
	"edits":      true,
	"oldString":  true,
	"newString":  true,
	"patchText":  true,
	"new_source": true, // NotebookEdit's cell body
	"old_source": true,
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
	if openCodeTypes[strField(payload, "type")] {
		return "opencode"
	}
	if _, ok := payload["turn_id"]; ok && (claudeEvents[name] || codexEvents[name]) {
		return "codex" // Codex hooks share Claude's event names; turn_id is Codex's
	}
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

// toolKind says whether a tool call asks the human something. "Task" and
// its friends contain "ask" but are agent work, not questions; an MCP
// tool (server__ask_user) is judged by its own name.
func toolKind(tool string) string {
	lowered := strings.ToLower(tool)
	name := lowered
	if i := strings.LastIndex(name, "__"); i >= 0 {
		name = name[i+2:]
	}
	if strings.HasPrefix(name, "ask") || strings.Contains(lowered, "question") {
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
	for _, key := range []string{"file_path", "filePath", "path", "notebook_path"} {
		if p := strField(data, key); p != "" {
			return p
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "…"
	}
	return s
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
	if qs, ok := data["questions"].([]any); ok {
		var parts []string
		for _, item := range qs {
			if q := strField(asDict(item), "question"); q != "" {
				parts = append(parts, q)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	}
	_, hasCommand := data["command"]
	if _, ok := data["cmd"]; ok {
		hasCommand = true
	}
	if u := strField(data, "url"); u != "" && !hasCommand {
		return tool + " " + u // WebFetch: the page, not its prompt
	}
	if p := strField(data, "prompt"); p != "" {
		return p
	}
	if hasCommand {
		return asCommand(data)
	}
	if p := strField(data, "file_path"); p != "" {
		return tool + " " + p
	}
	if p := strField(data, "path"); p != "" {
		return tool + " " + p
	}
	if p := strField(data, "notebook_path"); p != "" {
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
			if interrupted, _ := payload["is_interrupt"].(bool); interrupted {
				// The human stopped the turn; the agent is not carrying on.
				ev := NewEvent("cursor", "idle", "turn interrupted", map[string]any{"status": "aborted"})
				return &ev
			}
			text = tool + " failed"
			if path != "" {
				text += " · " + path
			}
			if msg := firstLine(strField(payload, "error_message")); msg != "" {
				text += ": " + msg
			}
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
		// End of turn. Never text: Cursor's concatenated message hid the
		// earlier replies. It only moves the turn to idle (or error) so the
		// dino stops typing even when no reply arrived.
		status := strings.TrimSpace(strField(payload, "status"))
		if status == "error" {
			ev := NewEvent("cursor", "error", "turn ended with an error", map[string]any{"status": status})
			return &ev
		}
		if status == "" {
			status = "completed"
		}
		ev := NewEvent("cursor", "idle", "turn "+status, map[string]any{"status": status})
		return &ev
	}
	return nil
}

func claudeTool(payload map[string]any) (string, any) {
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
	return tool, input
}

// MapClaude maps Claude Code hook payloads. Claude has no hook for
// thinking, so AGENT shows prompts, tool calls, results and the final
// reply (Stop's last_assistant_message), never a transcript.
func MapClaude(payload map[string]any) *Event {
	if _, fromCursor := payload["cursor_version"]; fromCursor {
		// Cursor runs Claude Code hooks too; its own hooks already report this.
		return nil
	}
	name := hookName(payload)
	switch name {
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
	case "PreToolUse":
		tool, input := claudeTool(payload)
		text := strings.TrimSpace(claudeToolText(tool, input))
		if text == "" {
			return nil
		}
		ev := NewEvent("claude", toolKind(tool), text, toolMeta(tool, payloadPath(payload)))
		return &ev
	case "PostToolUse":
		tool, input := claudeTool(payload)
		path := payloadPath(payload)
		meta := toolMeta(tool, path)
		meta["ok"] = true
		text := tool
		switch {
		case IsFileEditTool(tool) && path != "":
			text = "wrote " + path
		case IsShellTool(tool):
			if cmd := strings.TrimSpace(asCommand(asDict(input)["command"])); cmd != "" {
				text = "ran " + cmd
				meta["command"] = cmd
			}
		case path != "":
			text = tool + " " + path
		}
		ev := NewEvent("claude", "result", text, meta)
		return &ev
	case "PostToolUseFailure":
		tool, input := claudeTool(payload)
		if interrupted, _ := payload["is_interrupt"].(bool); interrupted {
			ev := NewEvent("claude", "idle", "turn interrupted", map[string]any{"status": "aborted"})
			return &ev
		}
		path := payloadPath(payload)
		meta := toolMeta(tool, path)
		meta["ok"] = false
		text := tool + " failed"
		if cmd := strings.TrimSpace(asCommand(asDict(input)["command"])); IsShellTool(tool) && cmd != "" {
			text += " · " + cmd
			meta["command"] = cmd
		} else if path != "" {
			text += " · " + path
		}
		if msg := firstLine(strField(payload, "error")); msg != "" {
			text += ": " + msg
		}
		ev := NewEvent("claude", "error", text, meta)
		return &ev
	case "Notification":
		text := strings.TrimSpace(strField(payload, "message"))
		switch strField(payload, "notification_type") {
		case "idle_prompt":
			ev := NewEvent("claude", "idle", "waiting for you", nil)
			return &ev
		case "", "permission_prompt", "elicitation_dialog", "agent_needs_input":
		default:
			return nil // auth, quota and plugin notices are not the agent asking
		}
		if text == "" {
			text = strings.TrimSpace(strField(payload, "text"))
		}
		if text == "" {
			return nil
		}
		ev := NewEvent("claude", "question", text, nil)
		return &ev
	case "Stop":
		if text := strings.TrimSpace(strField(payload, "last_assistant_message")); text != "" {
			ev := NewEvent("claude", "text", text, nil)
			return &ev
		}
		ev := NewEvent("claude", "idle", "turn completed", nil)
		return &ev
	case "StopFailure":
		text := "turn failed"
		if msg := firstLine(strField(payload, "error")); msg != "" {
			text += ": " + msg
		}
		ev := NewEvent("claude", "error", text, nil)
		return &ev
	case "SessionStart":
		if strField(payload, "source") == "compact" {
			return nil // a compaction mid-turn; the turn goes on
		}
		ev := NewEvent("claude", "idle", "session started", nil)
		return &ev
	case "SessionEnd":
		ev := NewEvent("claude", "idle", "session ended", nil)
		return &ev
	}
	return nil
}

// claudeToolText is a short line for one of Claude Code's own tools: a
// plan, a todo list or a subagent prompt is never flattened onto AGENT,
// and nothing falls back to a JSON dump of the input.
func claudeToolText(tool string, input any) string {
	data := asDict(input)
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := firstLine(strField(data, k)); v != "" {
				return v
			}
		}
		return ""
	}
	labelled := func(label, detail string) string {
		if detail == "" {
			return label
		}
		return label + " " + detail
	}
	switch tool {
	case "ExitPlanMode":
		return labelled("plan ready", first("planFilePath"))
	case "TodoWrite":
		todos, _ := data["todos"].([]any)
		for _, item := range todos {
			if t := asDict(item); strField(t, "status") == "in_progress" {
				return labelled("todo:", firstLine(strField(t, "content")))
			}
		}
		return "todo list updated"
	case "TaskCreate", "TaskUpdate", "TaskGet", "TaskList", "TaskOutput", "TaskStop":
		return labelled(tool, first("subject", "description", "taskId", "task_id"))
	case "Task", "Agent":
		return labelled(tool, first("description", "subagent_type"))
	case "Skill":
		return labelled(tool, first("skill"))
	}
	if strings.HasPrefix(tool, "mcp__") {
		if q := first("question"); q != "" {
			return q // an MCP tool that asks the human
		}
		name := strings.ReplaceAll(strings.TrimPrefix(tool, "mcp__"), "__", ".")
		return labelled(name, first("query", "url", "path", "file_path", "title", "name"))
	}
	text := toolText(tool, input)
	if t := strings.TrimSpace(text); strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return tool // an input with nothing to show by name: just the tool
	}
	return text
}

func MapCodex(payload map[string]any) *Event {
	if name := strField(payload, "hook_event_name"); name != "" {
		return mapCodexHook(payload, name)
	}
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
	case "opencode":
		return MapOpenCode(payload)
	default:
		return MapCursor(payload)
	}
}
