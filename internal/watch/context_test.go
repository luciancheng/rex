package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Only Claude Code's and Codex's prompt hooks, and their SessionStart
// after a compaction, ask for the pin (docs/ux.md §7a); every other event
// any harness sends, and every other source, is silent.
func TestContextEventIsAnAllowList(t *testing.T) {
	names := map[string]bool{}
	for _, set := range []map[string]bool{cursorEvents, claudeEvents, codexEvents, openCodeTypes} {
		for name := range set {
			names[name] = true
		}
	}
	for _, list := range [][]string{CursorHookEvents, ClaudeHookEvents, CodexHookEvents} {
		for _, name := range list {
			names[name] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, source := range []string{"", "auto", "cursor", "claude", "codex", "opencode"} {
		for _, name := range sorted {
			for _, from := range []string{"startup", "resume", "clear", "compact", ""} {
				payload := map[string]any{"hook_event_name": name, "type": name, "source": from, "turn_id": "t1", "cwd": "/repo", "prompt": "go"}
				want := ""
				if source == "claude" || source == "codex" {
					if name == "UserPromptSubmit" || name == "SessionStart" && from == "compact" {
						want = name
					}
				}
				if got := ContextEvent(payload, source); got != want {
					t.Errorf("%s from %s (source %q): %q, want %q", name, source, from, got, want)
				}
			}
		}
	}
	for _, quiet := range []map[string]any{
		{"hook_event_name": "UserPromptSubmit", "agent_id": "sub-1"},
		{"hook_event_name": "UserPromptSubmit", "cursor_version": "1.7"},
		{"hook_event_name": "SessionStart", "source": "compact", "agent_id": "sub-1"},
	} {
		for _, source := range []string{"claude", "codex"} {
			if got := ContextEvent(quiet, source); got != "" {
				t.Errorf("%v from %s: %q", quiet, source, got)
			}
		}
	}
}

// The reply shapes (docs/ux.md §7a): Claude Code gets exactly one JSON
// object, Codex the note as text that never starts like JSON.
func TestContextReplyShapes(t *testing.T) {
	note := "Rex note: \"quoted\" <b> ‹x› line one\nline two"
	for _, event := range []string{"UserPromptSubmit", "SessionStart"} {
		out := ContextReply("claude", event, note)
		if !bytes.HasSuffix(out, []byte("}\n")) || bytes.Count(out, []byte("\n")) != 1 {
			t.Fatalf("one line of JSON: %q", out)
		}
		dec := json.NewDecoder(bytes.NewReader(out))
		var got struct {
			Out struct {
				Event   string `json:"hookEventName"`
				Context string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := dec.Decode(&got); err != nil || got.Out.Event != event || got.Out.Context != note {
			t.Fatalf("%v %+v", err, got)
		}
		if _, err := dec.Token(); err != io.EOF {
			t.Fatalf("exactly one object: %q", out)
		}
		var keys map[string]map[string]any
		json.Unmarshal(out, &keys)
		if len(keys) != 1 || len(keys["hookSpecificOutput"]) != 2 {
			t.Fatalf("nothing else in it: %s", out)
		}
	}
	if got := string(ContextReply("codex", "UserPromptSubmit", note)); got != note+"\n" {
		t.Fatalf("codex: %q", got)
	}
	for _, bad := range []string{"{\"a\":1}", " [1]", "", "  \n"} {
		if got := ContextReply("codex", "UserPromptSubmit", bad); got != nil {
			t.Errorf("codex %q: %q", bad, got)
		}
	}
	for _, source := range []string{"cursor", "opencode", "auto", ""} {
		if got := ContextReply(source, "UserPromptSubmit", note); got != nil {
			t.Errorf("%s: %q", source, got)
		}
	}
}

// Codex trusts a hook by a hash of its handler, so handing over the pin
// changed neither its events nor its entry: a hooks.json an earlier Rex
// wrote is left byte for byte as it was (docs/ux.md §7a).
func TestCodexHooksFileIsUnchanged(t *testing.T) {
	entry := func(exe string) string {
		command, _ := json.Marshal(shellQuote(exe) + " ingest --source codex")
		var b strings.Builder
		b.WriteString("{\n  \"hooks\": {\n")
		names := []string{"PermissionRequest", "PostToolUse", "PreToolUse", "SessionStart", "Stop", "UserPromptSubmit"}
		for i, name := range names {
			fmt.Fprintf(&b, "    %q: [\n      {\n        \"hooks\": [\n          {\n            \"command\": %s,\n            \"timeout\": 5,\n            \"type\": \"command\"\n          }\n        ]\n      }\n    ]", name, command)
			if i < len(names)-1 {
				b.WriteString(",")
			}
			b.WriteString("\n")
		}
		b.WriteString("  }\n}\n")
		return b.String()
	}
	defer func(goos string) { runtimeGOOS = goos }(runtimeGOOS)
	runtimeGOOS = "darwin"
	home := codexHome(t)
	path := filepath.Join(home, ".codex", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := InstallHarness("codex", io.Discard); err != nil {
		t.Fatal(err)
	}
	exe := rexExecutable()
	if got, _ := os.ReadFile(path); string(got) != entry(exe) {
		t.Fatalf("a fresh install:\n%s\nwant:\n%s", got, entry(exe))
	}
	var out bytes.Buffer
	if err := InstallHarness("codex", &out); err != nil || !strings.Contains(out.String(), "codex hooks already installed") || !strings.Contains(out.String(), "the hook also adds the task pinned in this repo's Rex plan") {
		t.Fatalf("%v %q", err, out.String())
	}
	if got, _ := os.ReadFile(path); string(got) != entry(exe) {
		t.Fatal("reinstalling changed it")
	}
}

// Installing says the hook now hands over the pin, since a hook that
// only watched before does more now.
func TestInstallSaysTheHookAddsThePin(t *testing.T) {
	home := claudeHome(t)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	var out bytes.Buffer
	if err := InstallHarness("claude", &out); err != nil || !strings.Contains(out.String(), "claude code: before each prompt the hook also adds the task pinned in this repo's Rex plan") {
		t.Fatalf("%v %q", err, out.String())
	}
}
