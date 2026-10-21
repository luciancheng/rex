package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestCodexPatchHeadersAreOnlyHeaders(t *testing.T) {
	patch := "*** Begin Patch\n*** Update File: docs/apply.md\n@@\n context\n *** Delete File: README.md\n+*** Add File: fake.go\n*** End Patch\n"
	files := patchFiles(patch, "/r")
	if len(files) != 1 || files[0].path != filepath.Join("/r", "docs/apply.md") {
		t.Fatalf("a context or body line that looks like a header is no file: %+v", files)
	}
}

func TestCodexFailedPatchIsNoWrite(t *testing.T) {
	ev := codexMap(t, `{"hook_event_name":"PostToolUse","turn_id":"t","cwd":"/r","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: a.go\n*** End Patch"},"tool_response":"Failed to read file to update /r/a.go"}`)
	if ev == nil || ev.Kind != "error" || ev.Meta["ok"] != false || !strings.Contains(ev.Text, "apply_patch failed") {
		t.Fatalf("%#v", ev)
	}
	patch := `{"hook_event_name":"PostToolUse","turn_id":"t","cwd":"/r","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: a.go\n*** End Patch"},"tool_response":%q}`
	for resp, failed := range map[string]bool{
		"Success. Updated the following files:\nM a.go": false,
		// how Codex formats tool output for the model, and so for the hook
		"Exit code: 0\nWall time: 0.1 seconds\nOutput:\nSuccess. Updated the following files:\nM a.go\n": false,
		"Exit code: 1\nWall time: 0 seconds\nOutput:\nFailed to find expected lines in /r/a.go":          true,
		"Exit code: 0\nWall time: 0 seconds\nOutput:\napply_patch verification failed: no such file":     true,
		"done": false, // anything unrecognised is a write, as it always was
		`{"output":"Success. Updated the following files:\nM a.go","metadata":{"exit_code":0,"duration_seconds":0.1}}`: false,
		`{"output":"Failed to find expected lines","metadata":{"exit_code":1,"duration_seconds":0}}`:                   true,
		"patch rejected by user": true,
	} {
		ev := codexMap(t, fmt.Sprintf(patch, resp))
		if ev == nil || (ev.Kind == "error") != failed || ev.Meta["ok"] != !failed {
			t.Errorf("%q: %#v", resp, ev)
		}
		if failed && (strings.Contains(ev.Text, "Exit code") || strings.Contains(ev.Text, "{")) {
			t.Errorf("the tape shows the patch's own reason: %q", ev.Text)
		}
	}
	// A JSON-shaped result can also come as an object.
	obj := codexMap(t, `{"hook_event_name":"PostToolUse","turn_id":"t","cwd":"/r","tool_name":"apply_patch","tool_input":{"command":"*** Begin Patch\n*** Update File: a.go\n*** End Patch"},"tool_response":{"output":"Failed to find expected lines","metadata":{"exit_code":1}}}`)
	if obj == nil || obj.Kind != "error" || !strings.Contains(obj.Text, "Failed to find expected lines") {
		t.Fatalf("%#v", obj)
	}
}

func TestCodexCompactionKeepsTheTurn(t *testing.T) {
	if ev := codexMap(t, `{"hook_event_name":"SessionStart","turn_id":"t","source":"compact"}`); ev != nil {
		t.Fatalf("a compaction mid-turn is no turn end: %#v", ev)
	}
	if ev := codexMap(t, `{"hook_event_name":"SessionStart","turn_id":"t","source":"startup"}`); ev == nil || ev.Kind != "idle" {
		t.Fatalf("%#v", ev)
	}
}

func TestCodexToolLinesAreNeverJSON(t *testing.T) {
	for _, payload := range []string{
		`{"hook_event_name":"PreToolUse","turn_id":"t","tool_name":"update_plan","tool_input":{"explanation":"x","plan":[{"step":"read","status":"completed"},{"step":"write the mapper","status":"in_progress"}]}}`,
		`{"hook_event_name":"PreToolUse","turn_id":"t","tool_name":"mcp__github__get_issue","tool_input":{"owner":"x","body":"SECRET BODY"}}`,
		`{"hook_event_name":"PreToolUse","turn_id":"t","tool_name":"write_stdin","tool_input":{"session_id":3,"chars":"y"}}`,
	} {
		ev := codexMap(t, payload)
		if ev == nil || strings.HasPrefix(strings.TrimSpace(ev.Text), "{") || strings.Contains(ev.Text, "SECRET BODY") {
			t.Errorf("%s → %#v", payload, ev)
		}
	}
	if ev := codexMap(t, `{"hook_event_name":"PreToolUse","turn_id":"t","tool_name":"update_plan","tool_input":{"plan":[{"step":"write the mapper","status":"in_progress"}]}}`); ev.Text != "plan: write the mapper" {
		t.Fatalf("%q", ev.Text)
	}
}

func TestCodexAutoDetectsEveryHook(t *testing.T) {
	if got := DetectSource(map[string]any{"hook_event_name": "PermissionRequest", "turn_id": "t"}, "auto"); got != "codex" {
		t.Fatalf("PermissionRequest with turn_id is Codex's: %q", got)
	}
}

func TestCodexWiredReadsLoosely(t *testing.T) {
	home := codexHome(t)
	path := filepath.Join(home, ".codex", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	cmd, _ := json.Marshal(codexHandler(rexExecutable(), runtimeGOOS)["command"])
	os.WriteFile(path, []byte(`{"hooks": {"Odd": "not a list", "Stop": [{"hooks": [{"type": "command", "command": `+string(cmd)+`}]}]}}`), 0o644)
	if !codexWired(path) {
		t.Fatal("one odd entry of the user's doesn't hide Rex's")
	}
}

func TestCodexBigPatchIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("*** Begin Patch\n")
	for i := 0; i < maxPatchPaths+50; i++ {
		fmt.Fprintf(&b, "*** Add File: f%d.go\n+x\n", i)
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "turn_id": "t", "cwd": "/r", "tool_name": "apply_patch", "tool_input": map[string]any{"command": b.String()}})
	ev := codexMap(t, string(payload))
	if paths := ev.Meta["paths"].([]any); len(paths) != maxPatchPaths {
		t.Fatalf("%d paths", len(paths))
	}
}
