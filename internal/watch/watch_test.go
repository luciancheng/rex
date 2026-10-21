package watch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
	if EventPath(*event) != "/tmp/note.md" {
		t.Fatalf("path %q", EventPath(*event))
	}
}

func TestCursorAfterFileEditIsResult(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "afterFileEdit",
		"file_path":       "/tmp/note.md",
		"tool_name":       "Write",
	}, "auto")
	if event == nil || event.Kind != "result" {
		t.Fatalf("%#v", event)
	}
	if event.Text != "wrote /tmp/note.md" {
		t.Fatalf("text %q", event.Text)
	}
	if !IsFileEditEvent(*event) {
		t.Fatal("should count as a file edit")
	}
	if EventPath(*event) != "/tmp/note.md" {
		t.Fatalf("path %q", EventPath(*event))
	}
}

func TestCursorAfterAgentResponse(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "afterAgentResponse",
		"text":            "Typing means a file edit is in flight.",
	}, "auto")
	if event == nil || event.Kind != "text" || !contains(event.Text, "file edit") {
		t.Fatalf("%#v", event)
	}
}

func TestCursorAfterAgentResponseContentBlocks(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "afterAgentResponse",
		"content": []any{
			map[string]any{"type": "text", "text": "Final reply from blocks."},
		},
	}, "auto")
	if event == nil || event.Kind != "text" || event.Text != "Final reply from blocks." {
		t.Fatalf("%#v", event)
	}
}

func TestCursorStopDoesNotBecomeReply(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "stop",
		"status":          "completed",
		"text":            "Here is the answer.",
	}, "auto")
	if event != nil {
		t.Fatalf("stop should not replace replies %#v", event)
	}
}

func TestCursorPostToolUseWrite(t *testing.T) {
	event := MapPayload(map[string]any{
		"hook_event_name": "postToolUse",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"path": "/tmp/note.md", "contents": "x"},
	}, "auto")
	if event == nil || event.Kind != "result" || !contains(event.Text, "/tmp/note.md") {
		t.Fatalf("%#v", event)
	}
	if contains(event.Text, "contents") || event.Text == "x" {
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
	blocked := MapPayload(map[string]any{
		"hook_event_name": "afterAgentThought",
		"content": []any{
			map[string]any{"type": "text", "text": "Checking the hook payload."},
		},
	}, "auto")
	if blocked == nil || blocked.Kind != "thought" || blocked.Text != "Checking the hook payload." {
		t.Fatalf("thought blocks %#v", blocked)
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

func TestEventTailKeepsIncompleteLine(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(feed, []byte(`{"source":"cursor","kind":"text","text":"Hel`), 0o644); err != nil {
		t.Fatal(err)
	}
	tail := NewEventTail(feed, false)
	if got := tail.Poll(); len(got) != 0 {
		t.Fatalf("partial line leaked %#v", got)
	}
	f, err := os.OpenFile(feed, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`lo"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	got := tail.Poll()
	if len(got) != 1 || got[0].Text != "Hello" {
		t.Fatalf("%#v", got)
	}
}

func TestAppendKeepsGrowingReplyAppendOnly(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	tail := NewEventTail(feed, false)
	if _, err := AppendEvent(feed, NewEvent("cursor", "text", "Typing now means", nil)); err != nil {
		t.Fatal(err)
	}
	first := tail.Poll()
	ok, err := AppendEvent(feed, NewEvent("cursor", "text", "Typing now means the agent is writing.", nil))
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	second := tail.Poll()
	data, _ := os.ReadFile(feed)
	if lines := nonempty(string(data)); len(lines) != 2 {
		t.Fatalf("want both lines kept, got %d: %s", len(lines), data)
	}
	if len(first) != 1 || len(second) != 1 || !strings.Contains(second[0].Text, "the agent is writing") {
		t.Fatalf("tail lost the growing reply: %#v %#v", first, second)
	}
	if !IsGrowingText(first[0], second[0]) {
		t.Fatal("second line should read as a growing reply")
	}
}

func TestLastEventsReadsOnlyTheTail(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	filler := strings.Repeat("x", 1000)
	f, err := os.Create(feed)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		line, _ := NewEvent("cursor", "thought", fmt.Sprintf("%d %s", i, filler), nil).ToJSON()
		f.WriteString(line + "\n")
	}
	f.Close()
	got := LastEvents(feed, 3)
	if len(got) != 3 || !strings.HasPrefix(got[2].Text, "199 ") || !strings.HasPrefix(got[0].Text, "197 ") {
		t.Fatalf("%d events, last %q", len(got), got[len(got)-1].Text[:8])
	}
}

func TestAppendCapsHugeText(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	huge := strings.Repeat("é", maxEventText)
	if _, err := AppendEvent(feed, NewEvent("cursor", "text", huge, nil)); err != nil {
		t.Fatal(err)
	}
	got := LastEvents(feed, 1)
	if len(got) != 1 {
		t.Fatal("capped event should still fit the tail window")
	}
	if len(got[0].Text) > maxEventText+len("…") || !utf8.ValidString(got[0].Text) || !strings.HasSuffix(got[0].Text, "…") {
		t.Fatalf("len=%d", len(got[0].Text))
	}
}

func TestFeedIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	dir := filepath.Join(t.TempDir(), "rex")
	feed := filepath.Join(dir, "session.jsonl")
	if _, err := AppendEvent(feed, NewEvent("cursor", "user", "secret prompt", nil)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, feed: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestPayloadsFromRawStripsBOM(t *testing.T) {
	got := PayloadsFromRaw("\uFEFF{\"hook_event_name\":\"beforeSubmitPrompt\",\"prompt\":\"hi\"}")
	if len(got) != 1 || got[0]["prompt"] != "hi" {
		t.Fatalf("%#v", got)
	}
}

func TestReadPayloadDoesNotWaitForEOF(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	go w.Write([]byte(`{"hook_event_name":"stop"}`))
	start := time.Now()
	got := readPayload(r, 5*time.Second)
	if string(got) != `{"hook_event_name":"stop"}` {
		t.Fatalf("%q", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waited %v for a stdin that never closed", elapsed)
	}
}

func TestReadPayloadGivesUpAtDeadline(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	go w.Write([]byte(`{"hook_event_name":`))
	start := time.Now()
	got := readPayload(r, 150*time.Millisecond)
	if string(got) != `{"hook_event_name":` {
		t.Fatalf("%q", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline ignored: %v", elapsed)
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

func TestEventTailDropsPartialLineAfterTruncate(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(feed, []byte(`{"source":"cursor","kind":"user","text":"aaaaaaaaaaaaaaaaaaaaaaaa"}`+"\n"+`{"source":"cursor","kind":"user","te`), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := NewEventTail(feed, false)
	if got := tail.Poll(); len(got) != 1 {
		t.Fatalf("first poll %#v", got)
	}
	if err := os.WriteFile(feed, []byte(`{"source":"cursor","kind":"user","text":"b"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := tail.Poll()
	if len(got) != 1 || got[0].Text != "b" {
		t.Fatalf("event after truncation lost: %#v", got)
	}
}

func TestLastEventsKeepsALineStartingExactlyAtTheWindow(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	first, _ := NewEvent("cursor", "user", "first", nil).ToJSON()
	filler, _ := NewEvent("cursor", "thought", strings.Repeat("x", 100), nil).ToJSON()
	window := first + "\n"
	for len(window)+len(filler)+1 <= tailBytes {
		window += filler + "\n"
	}
	window += strings.Repeat("q", tailBytes-len(window)-1) + "\n" // a junk line pads to exactly tailBytes
	if len(window) != tailBytes {
		t.Fatalf("setup: window is %d bytes", len(window))
	}
	content := `{"source":"cursor","kind":"user","text":"before"}` + "\n" + window
	if err := os.WriteFile(feed, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := LastEvents(feed, 10000)
	if len(got) == 0 || got[0].Text != "first" {
		t.Fatalf("the line starting exactly at the window was dropped: first=%q", got[0].Text)
	}
}

func TestFeedPermissionsTightenOnOldInstalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".rex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	feed := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(feed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendEvent(feed, NewEvent("cursor", "user", "private", nil)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, feed: 0o600} {
		if info, _ := os.Stat(path); info.Mode().Perm() != want {
			t.Fatalf("%s mode %o, want %o", path, info.Mode().Perm(), want)
		}
	}
}

func TestEventTailDropsPartialLineAfterDeleteAndRecreate(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	os.WriteFile(feed, []byte(`{"source":"cursor","kind":"user","text":"a"}`+"\n"+`{"source":"cursor","kind":"us`), 0o600)
	tail := NewEventTail(feed, false)
	tail.Poll()
	os.Remove(feed)
	tail.Poll()
	os.WriteFile(feed, []byte(`{"source":"cursor","kind":"user","text":"b"}`+"\n"), 0o600)
	if got := tail.Poll(); len(got) != 1 || got[0].Text != "b" {
		t.Fatalf("%#v", got)
	}
}
