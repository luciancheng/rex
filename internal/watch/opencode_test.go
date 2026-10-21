package watch

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func openCodeMap(t *testing.T, payload string) *Event {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	return MapPayload(m, "auto")
}

func TestMapOpenCode(t *testing.T) {
	cases := []struct{ payload, kind, text string }{
		{`{"type":"chat.message","cwd":"/repo","text":"fix the cards"}`, "user", "fix the cards"},
		{`{"type":"message.part.updated","part":{"type":"reasoning","text":"Checking the card."}}`, "thought", "Checking the card."},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"running","input":{"command":"go test ./..."}}}}`, "command", "go test ./..."},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."}}}}`, "result", "ran go test ./..."},
		{`{"type":"message.part.updated","cwd":"/repo","part":{"type":"tool","tool":"edit","state":{"status":"running","input":{"filePath":"internal/tui/live.go"}}}}`, "command", "edit /repo/internal/tui/live.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/repo/a.go"}}}}`, "result", "wrote /repo/a.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"apply_patch","state":{"status":"completed","input":{},"files":[{"path":"/repo/a.go","type":"update"},{"path":"/repo/b.go","type":"add"}]}}}`, "result", "wrote /repo/a.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"error","input":{"command":"go vet"},"error":"exit 1\nmore"}}}`, "error", "bash failed · go vet: exit 1"},
		{`{"type":"session.idle","properties":{"sessionID":"s"}}`, "idle", "turn completed"},
		{`{"type":"session.error","error":{"name":"APIError","message":"rate limited\nresponse body"}}`, "error", "session error: rate limited"},
		{`{"type":"session.error","error":{"name":"MessageAbortedError"}}`, "idle", "turn interrupted"},
		{`{"type":"session.idle","sessionID":"s","text":"All done."}`, "text", "All done."},
		{`{"type":"permission.asked","properties":{"permission":"bash","patterns":["rm -rf build"]}}`, "question", "permission: bash: rm -rf build"},
		{`{"type":"permission.updated","properties":{"title":"Run rm -rf build?"}}`, "question", "permission: Run rm -rf build?"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"todowrite","state":{"status":"running","input":{"todos":[{"content":"write the mapper","status":"in_progress"}]}}}}`, "command", "todo: write the mapper"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"task","state":{"status":"running","input":{"description":"explore the repo","prompt":"long"}}}}`, "command", "task explore the repo"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"apply_patch","state":{"status":"completed","input":{},"files":[{"path":"/repo/old.go","type":"delete"}]}}}`, "result", "deleted /repo/old.go"},
		{`{"type":"question.asked","properties":{"questions":[{"question":"Which plan?"}]}}`, "question", "Which plan?"},
	}
	for _, c := range cases {
		ev := openCodeMap(t, c.payload)
		if ev == nil || ev.Kind != c.kind || !strings.Contains(ev.Text, c.text) || ev.Source != "opencode" {
			t.Errorf("%s\n got %#v", c.payload, ev)
		}
	}
	var multi string
	for _, c := range cases {
		if strings.Contains(c.payload, `"/repo/b.go"`) {
			multi = c.payload
		}
	}
	patch := openCodeMap(t, multi)
	if got := EventPaths(*patch); strings.Join(got, ",") != "/repo/a.go,/repo/b.go" || patch.Meta["ok"] != true {
		t.Fatalf("%+v", patch)
	}
	if ev := openCodeMap(t, `{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"pending"}}}`); ev != nil {
		t.Fatal("pending tools are not shown")
	}
}

func openCodeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	return home
}

func TestInstallOpenCode(t *testing.T) {
	home := openCodeHome(t)
	var out bytes.Buffer
	if err := InstallHarness("opencode", &out); err != nil || !strings.Contains(out.String(), "nothing changed") {
		t.Fatalf("%v %q", err, out.String())
	}
	os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755)
	if err := InstallHarness("opencode", io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config", "opencode", "plugins", "rex.js")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(data, []byte(opencodeMarker)) || !strings.Contains(string(data), `"ingest", "--source", "opencode"`) {
		t.Fatalf("%v\n%s", err, data)
	}
	out.Reset()
	InstallHarness("opencode", &out)
	if !strings.Contains(out.String(), "already installed") || !harnesses["opencode"].Wired(home) {
		t.Fatal(out.String())
	}
	if node, err := exec.LookPath("node"); err == nil {
		mjs := filepath.Join(t.TempDir(), "rex.mjs")
		os.WriteFile(mjs, data, 0o644)
		if out, err := exec.Command(node, "--check", mjs).CombinedOutput(); err != nil {
			t.Fatalf("node --check: %v\n%s", err, out)
		}
	}
	if err := UninstallHarness("opencode", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("uninstall removes the plugin")
	}
}

func TestOpenCodeLeavesAForeignRexJSAlone(t *testing.T) {
	home := openCodeHome(t)
	x := filepath.Join(home, "xdg")
	t.Setenv("XDG_CONFIG_HOME", x)
	path := filepath.Join(x, "opencode", "plugins", "rex.js")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("// my own rex plugin\n"), 0o644)
	var out bytes.Buffer
	if err := InstallHarness("opencode", &out); err == nil && !strings.Contains(out.String(), "Rex didn't write it") {
		t.Fatalf("the refusal is reported: %q", out.String())
	}
	if data, _ := os.ReadFile(path); string(data) != "// my own rex plugin\n" {
		t.Fatal("a rex.js Rex didn't write is never overwritten")
	}
	if err := UninstallHarness("opencode", io.Discard); err == nil {
		t.Fatal("and never removed")
	}
}

func TestOpenCodeLeavesWhatItCantReadOrFindAlone(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs file modes that bind")
	}
	home := openCodeHome(t)
	path := filepath.Join(home, ".config", "opencode", "plugins", "rex.js")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(opencodeMarker+"\nold\n"), 0o000)
	if InstallHarness("opencode", io.Discard) == nil || UninstallHarness("opencode", io.Discard) == nil {
		t.Fatal("a rex.js Rex can't read is neither replaced nor removed")
	}
	os.Chmod(path, 0o644)
	if data, _ := os.ReadFile(path); string(data) != opencodeMarker+"\nold\n" {
		t.Fatal("it is left as it was")
	}
	os.Remove(path)
	os.Symlink(filepath.Join(home, "dotfiles", "rex.js"), path) // nothing there
	if err := InstallHarness("opencode", io.Discard); err == nil {
		t.Fatal("a link to a missing file is refused, not replaced")
	}
	if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link stays a link")
	}
}

func TestOpenCodeSessionErrorsAndExitCodes(t *testing.T) {
	if ev := openCodeMap(t, `{"type":"session.error","error":{}}`); ev == nil || ev.Text != "session error" {
		t.Fatalf("an error with nothing in it still reads whole: %#v", ev)
	}
	if ev := openCodeMap(t, `{"type":"message.part.updated","part":{"type":"tool","tool":"question","state":{"status":"running","input":{}}}}`); ev != nil {
		t.Fatalf("question.asked carries the question; its running tool part adds nothing: %#v", ev)
	}
	if ev := openCodeMap(t, `{"type":"message.part.updated","part":{"type":"tool","tool":"question","state":{"status":"completed","input":{}}}}`); ev == nil || ev.Kind != "result" {
		t.Fatalf("the answered question is a result, so the turn leaves ask: %#v", ev)
	}
	if ev := openCodeMap(t, `{"type":"session.error","error":{"name":"ContextOverflowError","message":"too long"}}`); ev != nil {
		t.Fatalf("OpenCode compacts and goes on: %#v", ev)
	}
	ev := openCodeMap(t, `{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."},"exit":1}}}`)
	if ev.Meta["ok"] != false {
		t.Fatalf("a non-zero exit is a failed run: %#v", ev)
	}
}

// The plugin, run under node against a fake rex: what it actually sends.
func TestOpenCodePluginSendsOnlyWhatItShould(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs node and a POSIX shell")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "sent.jsonl")
	fake := filepath.Join(dir, "rex")
	os.WriteFile(fake, []byte("#!/bin/sh\ncat >> \""+out+"\"\n"), 0o755)
	os.WriteFile(filepath.Join(dir, "rex.mjs"), opencodeSource(fake), 0o644)
	driver := `import { RexPlugin } from "./rex.mjs"
const h = await RexPlugin({ directory: "/repo" })
const now = Date.now()
const ev = (type, properties) => h.event({ event: { type, properties } })
await ev("session.created", { info: { id: "child", parentID: "parent" } })
await ev("message.updated", { info: { id: "m1", role: "assistant" } })
await ev("message.part.updated", { part: { type: "tool", tool: "write", callID: "c1", sessionID: "parent", state: { status: "completed", input: { filePath: "a.go", content: "SECRET BODY" }, metadata: { diff: "SECRET BODY", files: [{ filePath: "a.go", type: "update", patch: "SECRET BODY" }] }, attachments: ["data:SECRET BODY"], output: "SECRET BODY" } } })
await ev("message.part.updated", { part: { type: "tool", tool: "write", callID: "c1", sessionID: "parent", state: { status: "completed", input: { filePath: "a.go" } } } })
await ev("message.part.updated", { part: { type: "tool", tool: "multiedit", callID: "c2", sessionID: "parent", state: { status: "running", input: { filePath: "b.go", edits: [{ oldString: "SECRET BODY", newString: "SECRET BODY" }] } } } })
await ev("message.part.updated", { part: { id: "r1", type: "reasoning", text: "thinking it over", messageID: "m1", sessionID: "parent", time: { end: now } } })
await ev("message.part.updated", { part: { id: "r1", type: "reasoning", text: "thinking it over", messageID: "m1", sessionID: "parent", time: { end: now } } })
await ev("message.part.updated", { part: { id: "p1", type: "text", text: "working on it", messageID: "m1", sessionID: "parent", time: { end: now } } })
await ev("message.part.updated", { part: { id: "p2", type: "text", text: "All done.", messageID: "m1", sessionID: "parent", time: { end: now } } })
await ev("message.part.updated", { part: { id: "p3", type: "text", text: "OLD REPLY", messageID: "m1", sessionID: "parent", time: { end: now - 3600000 } } })
await h["chat.message"]({ sessionID: "child" }, { parts: [{ type: "text", text: "SUBAGENT PROMPT" }] })
await h["chat.message"]({ sessionID: "parent" }, { parts: [{ type: "text", text: "fix the cards" }] })
await ev("session.idle", { sessionID: "child" })
await ev("permission.asked", { permission: "edit", patterns: ["a.go"], metadata: { diff: "SECRET BODY" }, sessionID: "parent" })
await ev("session.idle", { sessionID: "parent" })
await new Promise((r) => setTimeout(r, 3000))
`
	os.WriteFile(filepath.Join(dir, "driver.mjs"), []byte(driver), 0o644)
	cmd := exec.Command(node, filepath.Join(dir, "driver.mjs"))
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	data, _ := os.ReadFile(out)
	sent := string(data)
	for _, never := range []string{"SECRET BODY", "SUBAGENT PROMPT", "OLD REPLY", "working on it", `"sessionID":"child"`} {
		if strings.Contains(sent, never) {
			t.Fatalf("the plugin sent %q:\n%s", never, sent)
		}
	}
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(sent), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			t.Fatalf("not JSON: %q", line)
		}
		if ev := MapOpenCode(m); ev != nil {
			kinds = append(kinds, ev.Kind+": "+ev.Text)
		}
	}
	want := []string{"result: wrote /repo/a.go", "command: multiedit /repo/b.go", "thought: thinking it over", "user: fix the cards", "question: permission: edit: a.go", "text: All done."}
	if strings.Join(kinds, " | ") != strings.Join(want, " | ") {
		t.Fatalf("mapped:\n%s\nwant:\n%s", strings.Join(kinds, "\n"), strings.Join(want, "\n"))
	}
}

func TestOpenCodeInstallKeepsASymlink(t *testing.T) {
	home := openCodeHome(t)
	plugins := filepath.Join(home, ".config", "opencode", "plugins")
	os.MkdirAll(plugins, 0o755)
	real := filepath.Join(t.TempDir(), "rex.js")
	os.WriteFile(real, []byte(opencodeMarker+"\nold\n"), 0o644)
	link := filepath.Join(plugins, "rex.js")
	if os.Symlink(real, link) != nil {
		t.Skip("no symlinks")
	}
	if err := InstallHarness("opencode", io.Discard); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("a dotfiles symlink stays a symlink")
	}
	if data, _ := os.ReadFile(real); !strings.Contains(string(data), `"ingest", "--source", "opencode"`) {
		t.Fatal("the file it points at is updated")
	}
	var out bytes.Buffer
	if err := UninstallHarness("opencode", &out); err != nil || !strings.Contains(out.String(), "it was a link") {
		t.Fatalf("uninstall says the file it pointed to is kept: %v %q", err, out.String())
	}
	if _, err := os.Stat(real); err != nil {
		t.Fatal("and keeps it")
	}
}
