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
		{`{"hook_event_name":"PreToolUse","tool_name":"TaskCreate","tool_input":{"subject":"wire the outline","description":"SECRET BODY"}}`, "command", "TaskCreate wire the outline"},
		{`{"hook_event_name":"PreToolUse","tool_name":"NotebookEdit","tool_input":{"notebook_path":"/r/a.ipynb","cell_id":"c1","new_source":"SECRET BODY","edit_mode":"replace"}}`, "command", "NotebookEdit /r/a.ipynb"},
		{`{"hook_event_name":"PreToolUse","tool_name":"ExitPlanMode","tool_input":{"plan":"# Plan\n\nSECRET BODY","planFilePath":"/r/plan.md"}}`, "command", "plan ready /r/plan.md"},
		{`{"hook_event_name":"PreToolUse","tool_name":"TodoWrite","tool_input":{"todos":[{"content":"SECRET BODY","status":"completed"},{"content":"write the tests","status":"in_progress"}]}}`, "command", "todo: write the tests"},
		{`{"hook_event_name":"PreToolUse","tool_name":"Agent","tool_input":{"description":"find the mapper","prompt":"SECRET BODY\nlong prompt","subagent_type":"Explore"}}`, "command", "Agent find the mapper"},
		{`{"hook_event_name":"PreToolUse","tool_name":"Skill","tool_input":{"skill":"rex-plan"}}`, "command", "Skill rex-plan"},
		{`{"hook_event_name":"PreToolUse","tool_name":"mcp__github__get_issue","tool_input":{"owner":"x","body":"SECRET BODY"}}`, "command", "github.get_issue"},
		{`{"hook_event_name":"PreToolUse","tool_name":"mcp__ui__ask_user","tool_input":{"question":"Ship it?"}}`, "question", "Ship it?"},
		{`{"hook_event_name":"SessionStart","source":"startup"}`, "idle", "session started"},
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
	if strings.Count(string(ex2), ".claude/settings.local.json\n") != 1 || strings.Count(string(ex2), ".claude/settings.local.json.bak\n") != 1 {
		t.Fatalf("each exclude line added once, the .bak too:\n%s", ex2)
	}
}

func TestInstallClaudeProjectInWorktreesAndSubmodules(t *testing.T) {
	claudeHome(t)
	// A worktree: .git is a file pointing at main/.git/worktrees/wt, whose
	// commondir leads back to main/.git, where info/exclude lives.
	main := t.TempDir()
	wtGit := filepath.Join(main, ".git", "worktrees", "wt")
	os.MkdirAll(wtGit, 0o755)
	os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o644)
	wt := t.TempDir()
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644)
	if err := InstallClaudeProject(wt, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ex, _ := os.ReadFile(filepath.Join(main, ".git", "info", "exclude")); !strings.Contains(string(ex), ".claude/settings.local.json") {
		t.Fatalf("worktree exclude %q", ex)
	}
	// A submodule: a relative gitdir with no commondir.
	super := t.TempDir()
	mod := filepath.Join(super, ".git", "modules", "sub")
	os.MkdirAll(mod, 0o755)
	sub := filepath.Join(super, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../.git/modules/sub\n"), 0o644)
	if err := InstallClaudeProject(sub, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ex, _ := os.ReadFile(filepath.Join(mod, "info", "exclude")); !strings.Contains(string(ex), ".claude/settings.local.json") {
		t.Fatalf("submodule exclude %q", ex)
	}
	// Outside git nothing is written.
	plain := t.TempDir()
	if err := InstallClaudeProject(plain, io.Discard); err == nil || !strings.Contains(err.Error(), "needs a git repo") {
		t.Fatalf("outside git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plain, ".claude")); err == nil {
		t.Fatal("nothing is written outside a repo")
	}
}

func TestClaudeKeepsTheUsersHandlersByteForByte(t *testing.T) {
	claudeHome(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	ours := claudeHandler(rexExecutable(), runtimeGOOS)
	cmd, _ := json.Marshal(ours["command"])
	user := `{"type":"command","command":"say done",   "timeout":   10.0}`
	body := `{
  "hooks": {
    "Stop": [
      {"hooks": [` + user + `, {"type":"command","command":` + string(cmd) + `}]}
    ]
  }
}
`
	os.WriteFile(path, []byte(body), 0o644)
	if err := uninstallClaude(path, io.Discard); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), user) || strings.Contains(string(got), "ingest --source claude") {
		t.Fatalf("the user's handler must keep its bytes and Rex's must go:\n%s", got)
	}
}

func TestClaudeReinstallKeepsRexsGroupInPlace(t *testing.T) {
	claudeHome(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := installClaude(path, io.Discard); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "\n") < 10 {
		t.Fatalf("a new settings file is written one entry per line:\n%s", data)
	}
	// The user lengthens Rex's timeout and adds a group after it.
	tweaked := strings.Replace(string(data), `"timeout": 5`, `"timeout": 30`, 1)
	tweaked = strings.Replace(tweaked, `"Stop": [`, `"Stop": [{"matcher":"x","hooks":[{"type":"command","command":"say hi"}]},`, 1)
	os.WriteFile(path, []byte(tweaked), 0o644)
	if err := installClaude(path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != tweaked {
		t.Fatalf("reinstall rewrote an installed hook:\n%s", again)
	}
	if err := uninstallClaude(path, io.Discard); err != nil {
		t.Fatal(err)
	}
	if out, _ := os.ReadFile(path); strings.Contains(string(out), "ingest --source claude") || !strings.Contains(string(out), "say hi") {
		t.Fatalf("uninstall:\n%s", out)
	}
	fresh := filepath.Join(t.TempDir(), "settings.json")
	installClaude(fresh, io.Discard)
	uninstallClaude(fresh, io.Discard)
	if out, _ := os.ReadFile(fresh); strings.Contains(string(out), "hooks") {
		t.Fatalf("uninstall leaves no empty hooks object:\n%s", out)
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
