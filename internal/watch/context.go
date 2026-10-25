package watch

import (
	"encoding/json"
	"strings"
)

// Pin context (docs/ux.md §7a): on the hooks whose stdout an agent reads
// as context, rex ingest may print the pin note. This file decides which
// hooks those are and shapes the reply for each harness; internal/plan
// finds the pin and words the note.

// ContextEvent is the hook event of payload whose stdout the agent adds to
// its context, or "" for every other: Claude Code's and Codex's
// UserPromptSubmit, and their SessionStart after a compaction. Only an
// explicit --source claude or codex counts, since the reply's shape is the
// harness's; a subagent's hooks (agent_id), and Cursor running Claude
// Code's (cursor_version, which Cursor's own hooks already cover), never
// ask.
func ContextEvent(payload map[string]any, source string) string {
	if source != "claude" && source != "codex" {
		return ""
	}
	if _, ok := payload["cursor_version"]; ok {
		return ""
	}
	if strField(payload, "agent_id") != "" {
		return ""
	}
	switch name := hookName(payload); {
	case name == "UserPromptSubmit":
		return name
	case name == "SessionStart" && strField(payload, "source") == "compact":
		return name
	}
	return ""
}

// ContextReply is what ingest prints to hand note to source's agent on
// event (one of ContextEvent's): for Claude Code, one JSON object whose
// additionalContext it adds unseen; for Codex, the note as plain text,
// which it adds as developer context. Codex reads stdout that starts with
// "{" or "[" as JSON, so such a note is never printed. It ends in a newline.
func ContextReply(source, event, note string) []byte {
	if strings.TrimSpace(note) == "" {
		return nil
	}
	switch source {
	case "claude":
		var reply struct {
			Out struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		reply.Out.Event, reply.Out.Context = event, note
		b, err := json.Marshal(reply)
		if err != nil {
			return nil
		}
		return append(b, '\n')
	case "codex":
		if lead := strings.TrimSpace(note); strings.HasPrefix(lead, "{") || strings.HasPrefix(lead, "[") {
			return nil
		}
		return []byte(note + "\n")
	}
	return nil
}
