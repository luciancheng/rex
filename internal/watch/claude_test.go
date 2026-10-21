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

func claudeMap(t *testing.T, payload string) *Event {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	return MapPayload(m, "claude")
}

func TestMapClaudeEvents(t *testing.T) {
	cases := []struct {
		payload, kind, text string
	}{
		{`{"hook_event_name":"UserPromptSubmit","prompt":"fix the cards"}`, "user", "fix the cards"},
		{`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`, "command", "go test ./..."},
		{`{"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"/r/a.go","content":"SECRET BODY"}}`, "command", "Write /r/a.go"},
		{`{"hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"/r/a.go","old_string":"x","new_string":"y"}}`, "command", "Edit /r/a.go"},
		{`{"hook_event_name":"PreToolUse","tool_name":"WebFetch","tool_input":{"url":"https://go.dev","prompt":"summarize"}}`, "command", "WebFetch https://go.dev"},
		{`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Which plan?"},{"question":"Ship it?"}]}}`, "question", "Which plan? · Ship it?"},
		{`{"hook_event_name":"PreToolUse","tool_name":"TaskCreate","tool_input":{"subject":"x"}}`, "command", ""},
		{`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"/r/a.go"},"tool_response":{"filePath":"/r/a.go"}}`, "result", "wrote /r/a.go"},
		{`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."},"tool_response":{"stdout":"ok"}}`, "result", "ran go test ./..."},
		{`{"hook_event_name":"PostToolUseFailure","tool_name":"Bash","tool_input":{"command":"go test ./..."},"error":"Exit code 1\nFAIL"}`, "error", "Bash failed · go test ./...: Exit code 1"},
		{`{"hook_event_name":"PostToolUseFailure","tool_name":"Bash","is_interrupt":true}`, "idle", "turn interrupted"},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude needs your permission to use Bash"}`, "question", "Claude needs your permission to use Bash"},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt","message":"Claude is waiting"}`, "idle", "waiting for you"},
		{`{"hook_event_name":"Stop","last_assistant_message":"The bar now spans all three rows."}`, "text", "The bar now spans all three rows."},
		{`{"hook_event_name":"Stop","last_assistant_message":""}`, "idle", "turn completed"},
		{`{"hook_event_name":"StopFailure","error":"rate limited"}`, "error", "turn failed: rate limited"},
		{`{"hook_event_name":"SessionEnd","reason":"other"}`, "idle", "session ended"},
	}
	for _, c := range cases {
		ev := claudeMap(t, c.payload)
		if ev == nil || ev.Kind != c.kind || (c.text != "" && ev.Text != c.text) || ev.Source != "claude" {
			t.Errorf("%s\n got %#v", c.payload, ev)
		}
		if ev != nil && strings.Contains(ev.Text, "SECRET BODY") {
			t.Error("write bodies never reach the tape")
		}
	}
	if ev := claudeMap(t, `{"hook_event_name":"Notification","notification_type":"auth_success","message":"ok"}`); ev != nil {
		t.Fatalf("auth notices are not questions: %#v", ev)
	}
	if ev := claudeMap(t, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"},"cursor_version":"2.1"}`); ev != nil {
		t.Fatal("Cursor running Claude hooks is already reported by Cursor's own hooks")
	}
	if ev := claudeMap(t, `{"hook_event_name":"PostToolUseFailure","tool_name":"Write","tool_input":{"file_path":"/r/a.go"},"error":"denied"}`); MetaTool(*ev) != "Write" {
		t.Fatal("a failed tool keeps its tool meta so the turn keeps running")
	}
}

func claudeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return home
}

func TestInstallClaudeSkipsWhenAbsent(t *testing.T) {
	home := claudeHome(t)
	var out bytes.Buffer
	if err := InstallHarness("claude", &out); err != nil || !strings.Contains(out.String(), "nothing changed") {
		t.Fatalf("%v %q", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("must not create ~/.claude")
	}
}

func TestInstallClaudeKeepsSettingsAndOtherHooks(t *testing.T) {
	home := claudeHome(t)
	path := filepath.Join(home, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	theirs := `{
  "permissions": {"defaultMode": "auto"},
  "enabledPlugins": {"frontend-design@claude-plugins-official": true},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "./guard.sh"}]},
      {"hooks": [{"type": "command", "command": "'/old/rex' ingest --source claude"}, {"type": "command", "command": "notify"}]}
    ]
  },
  "effortLevel": "high"
}
`
	os.WriteFile(path, []byte(theirs), 0o600)
	var out bytes.Buffer
	if err := InstallHarness("claude", &out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, keep := range []string{`"permissions": {"defaultMode": "auto"}`, `"enabledPlugins": {"frontend-design@claude-plugins-official": true}`, `"effortLevel": "high"`, `{"matcher": "Bash", "hooks": [{"type": "command", "command": "./guard.sh"}]}`} {
		if !bytes.Contains(data, []byte(keep)) {
			t.Fatalf("lost %s\n%s", keep, data)
		}
	}
	if bytes.Contains(data, []byte("/old/rex")) {
		t.Fatalf("stale rex handler left\n%s", data)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	hooks := settings["hooks"].(map[string]any)
	for _, name := range ClaudeHookEvents {
		if _, ok := hooks[name]; !ok {
			t.Fatalf("missing %s", name)
		}
	}
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 3 || !strings.Contains(string(mustJSON(pre[1])), "notify") || strings.Contains(string(mustJSON(pre[1])), "rex") {
		t.Fatalf("mixed group should keep notify only: %s", mustJSON(pre))
	}
	before, _ := os.ReadFile(path)
	InstallHarness("claude", io.Discard)
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("reinstall must be idempotent")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	out.Reset()
	if err := UninstallHarness("claude", &out); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "ingest --source claude") || !strings.Contains(string(data), "./guard.sh") || !strings.Contains(string(data), "notify") {
		t.Fatalf("uninstall\n%s", data)
	}
	if strings.Contains(string(data), `"Stop"`) {
		t.Fatalf("events Rex emptied are removed\n%s", data)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestInstallClaudeProjectStaysOutOfGit(t *testing.T) {
	claudeHome(t)
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git", "info"), 0o755)
	if err := InstallClaudeProject(repo, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
	ex, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if !strings.Contains(string(ex), ".claude/settings.local.json") {
		t.Fatalf("exclude %q", ex)
	}
	InstallClaudeProject(repo, io.Discard)
	ex2, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(ex2), "settings.local.json") != 1 {
		t.Fatal("exclude line added once")
	}
}

func TestClaudeHandlerForms(t *testing.T) {
	posix := claudeHandler("/Users/me/.local/bin/rex", "darwin")
	if posix["command"] != "'/Users/me/.local/bin/rex' ingest --source claude" || posix["args"] != nil {
		t.Fatalf("%#v", posix)
	}
	win := claudeHandler(`C:\Users\Jane Doe\rex.exe`, "windows")
	if win["command"] != `C:\Users\Jane Doe\rex.exe` || len(win["args"].([]any)) != 3 {
		t.Fatalf("windows uses exec form: %#v", win)
	}
	if !isRexClaudeHandler(map[string]any{"command": `C:\old\rex.exe`, "args": []any{"ingest", "--source", "claude"}}, posix) {
		t.Fatal("an exec-form rex handler from any path is ours")
	}
}
