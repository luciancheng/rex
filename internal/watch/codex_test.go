package watch

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexPatch = "*** Begin Patch\n*** Update File: internal/tui/live.go\n@@\n-SECRET OLD\n+SECRET NEW\n*** Add File: docs/new.md\n+hello\n*** Delete File: old.go\n*** End Patch"

func codexMap(t *testing.T, payload string) *Event {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	return MapPayload(m, "codex")
}

func TestMapCodexHooks(t *testing.T) {
	patch, _ := json.Marshal(codexPatch)
	cases := []struct{ payload, kind, text string }{
		{`{"hook_event_name":"UserPromptSubmit","turn_id":"t","prompt":"fix the cards"}`, "user", "fix the cards"},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`, "command", "go test ./..."},
		{`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_response":"ok"}`, "result", "ran go test ./..."},
		{`{"hook_event_name":"PreToolUse","cwd":"/repo","tool_name":"apply_patch","tool_input":{"command":` + string(patch) + `}}`, "command", "Edit /repo/internal/tui/live.go (+2 more)"},
		{`{"hook_event_name":"PostToolUse","cwd":"/repo","tool_name":"apply_patch","tool_input":{"command":` + string(patch) + `}}`, "result", "wrote /repo/internal/tui/live.go (+2 more)"},
		{`{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"rm -rf build"}}`, "question", "Bash wants permission: rm -rf build"},
		{`{"hook_event_name":"Stop","last_assistant_message":"Done.\naccomplished: x"}`, "text", "Done.\naccomplished: x"},
		{`{"hook_event_name":"Stop","last_assistant_message":null}`, "idle", "turn completed"},
		{`{"hook_event_name":"Interrupt"}`, "idle", "turn interrupted"},
		{`{"hook_event_name":"SessionStart","source":"startup"}`, "idle", "session started"},
	}
	for _, c := range cases {
		ev := codexMap(t, c.payload)
		if ev == nil || ev.Kind != c.kind || ev.Text != c.text || ev.Source != "codex" {
			t.Errorf("%s\n got %#v", c.payload, ev)
			continue
		}
		if strings.Contains(ev.Text, "SECRET") || strings.Contains(string(mustJSON(ev.Meta)), "SECRET") {
			t.Error("patch bodies never reach the tape")
		}
	}
	post := codexMap(t, `{"hook_event_name":"PostToolUse","cwd":"/repo","tool_name":"apply_patch","tool_input":{"command":`+string(patch)+`}}`)
	if got := EventPaths(*post); strings.Join(got, ",") != "/repo/internal/tui/live.go,/repo/docs/new.md,/repo/old.go" {
		t.Fatalf("%q", got)
	}
	if _, ok := codexMap(t, `{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"x"}}`).Meta["ok"]; ok {
		t.Fatal("Codex sends no exit code: a command result is never called passed")
	}
	if ev := codexMap(t, `{"hook_event_name":"PreCompact","trigger":"auto"}`); ev != nil {
		t.Fatalf("%#v", ev)
	}
	// The exec --json stream still works.
	if ev := codexMap(t, `{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}`); ev == nil || ev.Kind != "text" {
		t.Fatal("exec --json")
	}
	var m map[string]any
	json.Unmarshal([]byte(`{"hook_event_name":"UserPromptSubmit","turn_id":"t","prompt":"x"}`), &m)
	if DetectSource(m, "auto") != "codex" {
		t.Fatal("turn_id tells Codex from Claude")
	}
}

func codexHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	return home
}

func TestInstallCodex(t *testing.T) {
	home := codexHome(t)
	var out bytes.Buffer
	if err := InstallHarness("codex", &out); err != nil || !strings.Contains(out.String(), "nothing changed") {
		t.Fatalf("%v %q", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatal("must not create ~/.codex")
	}
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	out.Reset()
	if err := InstallHarness("codex", &out); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "hooks.json")
	data, _ := os.ReadFile(path)
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file) != 1 || file["hooks"] == nil {
		t.Fatalf("hooks.json may only hold description and hooks: %s", data)
	}
	for _, name := range CodexHookEvents {
		if _, ok := file["hooks"].(map[string]any)[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	if !strings.Contains(out.String(), "/hooks and trust") || !strings.Contains(out.String(), "login shell") {
		t.Fatalf("%q", out.String())
	}
	before := data
	InstallHarness("codex", io.Discard)
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("reinstall is byte-stable (Codex trusts by hash)")
	}
	if !harnesses["codex"].Wired(home) {
		t.Fatal("wired")
	}
}

func TestInstallCodexKeepsTheUsersHooks(t *testing.T) {
	home := codexHome(t)
	dir := filepath.Join(home, "elsewhere")
	t.Setenv("CODEX_HOME", dir)
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "hooks.json")
	theirs := `{
  "description": "my hooks",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "'/old/rex' ingest --source codex"}, {"type": "command", "command": "./guard.sh"}]}]
  }
}
`
	os.WriteFile(path, []byte(theirs), 0o600)
	if err := InstallHarness("codex", io.Discard); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, keep := range []string{`"description": "my hooks"`, "notify-send done", `{"type": "command", "command": "./guard.sh"}`, `"matcher": "Bash"`} {
		if !strings.Contains(string(data), keep) {
			t.Fatalf("lost %s\n%s", keep, data)
		}
	}
	if strings.Contains(string(data), "/old/rex") {
		t.Fatalf("stale rex handler left\n%s", data)
	}
	if err := UninstallHarness("codex", io.Discard); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "ingest --source codex") || !strings.Contains(string(data), "notify-send done") || !strings.Contains(string(data), "./guard.sh") {
		t.Fatalf("uninstall\n%s", data)
	}
}

func TestCodexHandlerForms(t *testing.T) {
	posix := codexHandler("/Users/me/.local/bin/rex", "darwin")
	if posix["command"] != "'/Users/me/.local/bin/rex' ingest --source codex" || posix["commandWindows"] != nil {
		t.Fatalf("%#v", posix)
	}
	win := codexHandler(`C:\Users\Jane Doe\rex.exe`, "windows")
	if win["commandWindows"] != `"C:\Users\Jane Doe\rex.exe" ingest --source codex` {
		t.Fatalf("cmd.exe form: %#v", win)
	}
	if !isRexCodexHandler(map[string]any{"commandWindows": `"C:\old\rex.exe" ingest --source codex`}, posix) {
		t.Fatal("a rex handler from any path is ours")
	}
}
