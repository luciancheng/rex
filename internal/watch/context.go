package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Pin context (docs/ux.md §7a): on the hooks whose stdout an agent reads
// as context, rex ingest may print the pin note. This file decides which
// hooks those are, where their agent works, and shapes the reply for each
// harness; internal/plan finds the pin and words the note.

// ContextEvent is the hook event of payload whose stdout the agent adds to
// its context, or "" for every other: Claude Code's and Codex's
// UserPromptSubmit, and their SessionStart after a compaction; Cursor's
// beforeSubmitPrompt and sessionStart; the OpenCode plugin's "context"
// request, which it sends from chat.message before a prompt of its root
// session's and which is never an event. Only an explicit --source
// counts, since the reply's shape is the harness's. A subagent's hooks
// (agent_id), any harness's, and Cursor running Claude Code's
// (cursor_version, which Cursor's own hooks already cover), never ask;
// Cursor's own payloads all carry cursor_version.
func ContextEvent(payload map[string]any, source string) string {
	if strField(payload, "agent_id") != "" {
		return ""
	}
	name := hookName(payload)
	switch source {
	case "cursor":
		if name == "beforeSubmitPrompt" || name == "sessionStart" {
			return name
		}
		return ""
	case "opencode":
		if strField(payload, "type") == "context" {
			return "context"
		}
		return ""
	case "claude", "codex":
	default:
		return ""
	}
	if _, ok := payload["cursor_version"]; ok {
		return ""
	}
	switch {
	case name == "UserPromptSubmit":
		return name
	case name == "SessionStart" && strField(payload, "source") == "compact":
		return name
	}
	return ""
}

// IsSessionStart reports whether event (one of ContextEvent's) starts a
// session, whose context the agent keeps for the rest of it rather than
// for one prompt, so the note is worded for the start of a session.
func IsSessionStart(event string) bool {
	return event == "SessionStart" || event == "sessionStart"
}

// ContextFolders are the folders the agent behind payload works in, best
// first, to look its pin up from; never the hook's own folder. Claude Code,
// Codex and the OpenCode plugin give the payload's cwd. Cursor's user
// hooks run in ~/.cursor, so its folders are the payload's workspace
// roots, then $CURSOR_PROJECT_DIR. Only absolute paths count.
func ContextFolders(payload map[string]any, source string) []string {
	var dirs []string
	add := func(v any) {
		if s, _ := v.(string); filepath.IsAbs(s) {
			dirs = append(dirs, s)
		}
	}
	if source != "cursor" {
		add(payload["cwd"])
		return dirs
	}
	roots, _ := payload["workspace_roots"].([]any)
	for _, r := range roots {
		add(r)
	}
	add(os.Getenv("CURSOR_PROJECT_DIR"))
	return dirs
}

// ContextReply is what ingest prints to hand note to source's agent on
// event (one of ContextEvent's): for Claude Code, one JSON object whose
// additionalContext it adds unseen; for Codex, the note as plain text,
// which it adds as developer context; for Cursor, one JSON object that
// lets the prompt go on and carries the note as additional_context; for
// the OpenCode plugin, one JSON object with the note, which it adds,
// trimmed, to the prompt as a synthetic text part, and what the note
// names (carried: pin_plan, and pin), which it copies into the prompt's
// event (openCodeReply, SentPin). Codex reads stdout that starts with "{"
// or "[" as JSON, so such a note is never printed. It ends in a newline.
func ContextReply(source, event, note string, carried map[string]any) []byte {
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
	case "opencode":
		if event != "context" {
			return nil
		}
		return openCodeReply(note, carried)
	case "cursor":
		// Never "continue": false, which would block the prompt, and never
		// user_message: the person isn't shown anything. "continue": true
		// stays because Cursor's docs don't mark it optional and every build
		// seen adding the context got it; how Cursor merges it with another
		// hook's false is undocumented, so Rex's entry goes first in the
		// list (cursorFirstEvents, docs/ux.md §7a). sessionStart's
		// context is documented; beforeSubmitPrompt's is not, and a build that
		// doesn't read it ignores it.
		var reply any
		switch event {
		case "beforeSubmitPrompt":
			reply = struct {
				Continue bool   `json:"continue"`
				Context  string `json:"additional_context"`
			}{true, note}
		case "sessionStart":
			reply = struct {
				Context string `json:"additional_context"`
			}{note}
		default:
			return nil
		}
		b, err := json.Marshal(reply)
		if err != nil {
			return nil
		}
		return append(asciiJSON(b), '\n')
	}
	return nil
}

// asciiJSON is the JSON b with every character past ASCII escaped as
// \uXXXX (a surrogate pair past U+FFFF), so the shell Cursor runs hooks
// through (PowerShell on Windows) can't re-encode it on the way.
func asciiJSON(b []byte) []byte {
	var out bytes.Buffer
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		switch {
		case r < utf8.RuneSelf:
			out.WriteByte(b[0])
		case r > 0xFFFF:
			hi, lo := utf16.EncodeRune(r)
			fmt.Fprintf(&out, `\u%04x\u%04x`, hi, lo)
		default:
			fmt.Fprintf(&out, `\u%04x`, r)
		}
		b = b[n:]
	}
	return out.Bytes()
}
