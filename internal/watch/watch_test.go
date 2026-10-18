package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorUserPrompt(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "beforeSubmitPrompt",
		"prompt":          "can we format this",
	}, "auto")
	if event == nil || event.Kind != "user" || event.Text != "can we format this" {
		t.Fatalf("unexpected %#v", event)
	}
}

func TestCursorWriteSkipsFileBody(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "preToolUse",
		"tool_name":       "Write",
		"tool_input": map[string]any{
			"path":     "/tmp/note.md",
			"contents": "# huge dump\n" + repeat("x", 200),
		},
	}, "auto")
	if event == nil || event.Text != "Write /tmp/note.md" {
		t.Fatalf("unexpected %#v", event)
	}
	if event != nil && contains(event.Text, "huge dump") {
		t.Fatal("dumped write body")
	}
}

func TestDuplicateThoughtIsNotAppended(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	first := NewEvent("cursor", "thought", "Looking at sprites.py", nil)
	ok, err := AppendEvent(feed, first)
	if err != nil || !ok {
		t.Fatalf("first write: %v %v", ok, err)
	}
	ok, err = AppendEvent(feed, NewEvent("cursor", "thought", "Looking at sprites.py", nil))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("duplicate thought was appended")
	}
	data, _ := os.ReadFile(feed)
	lines := nonempty(string(data))
	if len(lines) != 1 {
		t.Fatalf("lines=%d", len(lines))
	}
}

func TestCursorThoughtAndShell(t *testing.T) {
	thought := MapPayload(map[string]any{
		"hook_event_name": "afterAgentThought",
		"text":            "Check the idle sprite.",
	}, "auto")
	if thought == nil || thought.Kind != "thought" || !contains(thought.Text, "idle sprite") {
		t.Fatalf("thought %#v", thought)
	}
	command := MapPayload(map[string]any{
		"hook_event_name": "beforeShellExecution",
		"command":         "pytest -q",
	}, "auto")
	if command == nil || command.Kind != "command" || command.Text != "pytest -q" {
		t.Fatalf("command %#v", command)
	}
}

func TestCursorReadToolShowsPath(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "preToolUse",
		"tool_name":       "Read",
		"tool_input":      map[string]any{"file_path": "/tmp/idle.png"},
	}, "auto")
	if event == nil || event.Kind != "command" || event.Text != "Read /tmp/idle.png" {
		t.Fatalf("%#v", event)
	}
}

func TestCursorQuestionTool(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "preToolUse",
		"tool_name":       "AskQuestion",
		"tool_input":      map[string]any{"question": "Install the hook?"},
	}, "auto")
	if event == nil || event.Kind != "question" || !contains(event.Text, "Install the hook?") {
		t.Fatalf("%#v", event)
	}
}

func TestClaudeAndCodexMap(t *testing.T) {
	claude := MapPayload(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "ls"},
	}, "auto")
	if claude == nil || claude.Source != "claude" || claude.Kind != "command" {
		t.Fatalf("claude %#v", claude)
	}
	codex := MapPayload(map[string]any{
		"type": "item.started",
		"item": map[string]any{"type": "reasoning", "text": "Looking at the file."},
	}, "auto")
	if codex == nil || codex.Source != "codex" || codex.Kind != "thought" {
		t.Fatalf("codex %#v", codex)
	}
}

func TestEventTailReadsNewLines(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	tail := NewEventTail(feed, false)
	if len(tail.Poll()) != 0 {
		t.Fatal("expected empty")
	}
	if _, err := AppendEvent(feed, NewEvent("cursor", "thought", "hello", nil)); err != nil {
		t.Fatal(err)
	}
	first := tail.Poll()
	if len(first) != 1 || first[0].Text != "hello" {
		t.Fatalf("%#v", first)
	}
	if len(tail.Poll()) != 0 {
		t.Fatal("expected no more")
	}
	if _, err := AppendEvent(feed, NewEvent("cursor", "command", "echo hi", nil)); err != nil {
		t.Fatal(err)
	}
	second := tail.Poll()
	if len(second) == 0 || second[0].Kind != "command" {
		t.Fatalf("%#v", second)
	}
}

func TestEventTailCanSkipBacklog(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	if _, err := AppendEvent(feed, NewEvent("cursor", "thought", "old", nil)); err != nil {
		t.Fatal(err)
	}
	tail := NewEventTail(feed, true)
	if len(tail.Poll()) != 0 {
		t.Fatal("expected skip")
	}
	if _, err := AppendEvent(feed, NewEvent("cursor", "thought", "new", nil)); err != nil {
		t.Fatal(err)
	}
	fresh := tail.Poll()
	if len(fresh) != 1 || fresh[0].Text != "new" {
		t.Fatalf("%#v", fresh)
	}
}

func TestMarkdownLinesHeadingsListsAndCode(t *testing.T) {
	lines := MarkdownLines(
		"## Summary\n" +
			"Here is **bold** and `inline` plus a [link](https://example.com).\n" +
			"\n" +
			"- first\n" +
			"- second\n" +
			"\n" +
			"```python\n" +
			"print('hi')\n" +
			"```\n",
	)
	joined := lines
	if !containsAny(joined, "Summary") {
		t.Fatalf("%v", lines)
	}
	found := false
	for _, line := range lines {
		if len(line) >= 10 && line[:10] == "Here is bo" || contains(line, "Here is bold and inline plus a link") {
			found = true
		}
	}
	if !found {
		t.Fatalf("inline md: %v", lines)
	}
	if !containsAny(lines, "• first") || !containsAny(lines, "• second") {
		t.Fatalf("lists: %v", lines)
	}
	code := false
	for _, line := range lines {
		if strings.HasPrefix(line, "│ ") && strings.Contains(line, "print") {
			code = true
		}
	}
	if !code {
		t.Fatalf("code fence: %v", lines)
	}
	for _, line := range lines {
		if contains(line, "**") || contains(line, "##") || contains(line, "```") {
			t.Fatalf("raw leftover %q", line)
		}
	}
}

func TestDuplicateThoughtsMatchEvenLater(t *testing.T) {
	first := Event{Source: "cursor", Kind: "thought", Text: "Check the idle sprite.", TS: 1.0}
	again := Event{Source: "cursor", Kind: "thought", Text: "Check  the idle sprite.", TS: 9.0}
	if !IsDuplicate(again, []Event{first}, 4) {
		t.Fatal("expected duplicate")
	}
	other := Event{Source: "cursor", Kind: "thought", Text: "Something else.", TS: 9.0}
	if IsDuplicate(other, []Event{first}, 4) {
		t.Fatal("expected different thought")
	}
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func containsAny(lines []string, sub string) bool {
	for _, line := range lines {
		if contains(line, sub) {
			return true
		}
	}
	return false
}

func nonempty(s string) []string {
	var lines []string
	for _, line := range splitLines(s) {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
