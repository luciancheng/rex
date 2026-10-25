package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
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
		{`{"type":"message.part.updated","cwd":"/repo","part":{"type":"tool","tool":"edit","state":{"status":"running","input":{"filePath":"internal/tui/live.go"}}}}`, "command", "edit " + filepath.Join("/repo", "internal", "tui", "live.go")}, // joined the OS's way
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

// What a prompt carried is ingest's to stamp, for an explicit --source
// opencode (SentPin, docs/ux.md §7a): mapping the prompt's event never
// takes a pin, a plan or a note from the plugin's payload, and the
// plugin's request for the note is never an event.
func TestMapOpenCodePin(t *testing.T) {
	for _, payload := range []string{
		`{"type":"chat.message","text":"go","pin":"card-highlight","pin_plan":"rex/plans/cuj/sendoff-demo.rex.json"}`,
		`{"type":"chat.message","text":"go","note":"Rex note: the person has rex/plans/team/auth.rex.json open in Rex; it has no tasks yet."}`,
		`{"type":"chat.message","text":"go"}`,
	} {
		ev := openCodeMap(t, payload)
		if ev == nil || ev.Kind != "user" || ev.Text != "go" || ev.Meta["pin"] != nil || ev.Meta["pin_plan"] != nil || ev.Meta["note"] != nil {
			t.Errorf("%s: %+v", payload, ev)
		}
	}
	for _, source := range []string{"opencode", "auto"} {
		var m map[string]any
		json.Unmarshal([]byte(`{"type":"context","cwd":"/repo","sessionID":"s","text":"go"}`), &m)
		if ev := MapPayload(m, source); ev != nil {
			t.Errorf("%s: the context request is no event: %+v", source, ev)
		}
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
	if err != nil || !bytes.HasPrefix(data, []byte(opencodeMarker+"\n")) || !strings.Contains(string(data), `"ingest", "--source", "opencode"`) {
		t.Fatalf("%v\n%s", err, data)
	}
	out.Reset()
	InstallHarness("opencode", &out)
	if !strings.Contains(out.String(), "already installed") || !harnesses["opencode"].Wired(home) || harnesses["opencode"].Old(home) {
		t.Fatal(out.String())
	}
	if !strings.Contains(out.String(), "opencode: before each prompt the plugin also adds the task pinned in this repo's Rex plan") {
		t.Fatalf("installing says the plugin hands over the pin: %q", out.String())
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

// The plugin, run under node against a fake rex (stubRex, this test
// binary): what it actually sends.
func TestOpenCodePluginSendsOnlyWhatItShould(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("needs node")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "sent.jsonl")
	os.WriteFile(filepath.Join(dir, "rex.mjs"), opencodeSource(self), 0o644)
	driver := `import { readFileSync } from "node:fs"
import { RexPlugin } from "./rex.mjs"
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
for (let i = 0; i < 600; i++) {
  let n = 0
  try { n = readFileSync(process.env.REX_TEST_STUB_DIR + "/sent.jsonl", "utf8").trim().split("\n").length } catch {}
  if (n >= 6) break
  await new Promise((r) => setTimeout(r, 50))
}
`
	os.WriteFile(filepath.Join(dir, "driver.mjs"), []byte(driver), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(dir, "driver.mjs"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "REX_TEST_STUB_DIR="+dir)
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
	// The plugin sends the tool's relative paths; they join to the session's
	// folder with the OS's separator.
	want := []string{"result: wrote " + filepath.Join("/repo", "a.go"), "command: multiedit " + filepath.Join("/repo", "b.go"), "thought: thinking it over", "user: fix the cards", "question: permission: edit: a.go", "text: All done."}
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

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The plugin as Rex writes it, byte for byte: the code that runs inside
// OpenCode. After a deliberate change (one OpenCode users should get also
// bumps opencodeVersion, so Harness.Old knows their plugin):
// go test ./internal/watch -run OpenCodePluginGolden -update
func TestOpenCodePluginGolden(t *testing.T) {
	got := opencodeSource("/opt/rex/bin/rex")
	golden := filepath.Join("testdata", "opencode-rex.js")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		if i >= len(g) || i >= len(w) || g[i] != w[i] {
			t.Fatalf("the plugin differs from %s at line %d (after a deliberate change, bump opencodeVersion if OpenCode users should get it, then rerun with -update):\n got %q\nwant %q", golden, i+1, line(g, i), line(w, i))
		}
	}
}

func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(end)"
}

// The plugin's bounds (docs/ux.md §7a): only chat.message waits, for rex
// at most 1.5 s, and reads at most 8 KB of its reply (openCodeCap), which
// it parses inside a catch; each hook's whole body is one try with an
// empty catch, so nothing it does can fail a prompt; and the only output
// it sets is one part pushed onto the prompt's own parts, never a new
// array.
func TestOpenCodePluginBounds(t *testing.T) {
	src := string(opencodeSource("rex"))
	if !strings.HasPrefix(src, "// generated by rex · plugin v2 — safe to delete\n") || pluginVersion([]byte(src)) != opencodeVersion {
		t.Fatalf("the marker names the plugin's version: %q", strings.SplitN(src, "\n", 2)[0])
	}
	for _, want := range []string{
		fmt.Sprintf("const WAIT = 1500, CAP = %d", openCodeCap),
		"const t = setTimeout(stop, WAIT)",
		"const stop = () => { try { c.kill() } catch {} ; end({}) }",
		"if (size <= CAP) chunks.push(d); else if (size - d.length <= CAP) stop()",
		`end(code === 0 && size <= CAP ? reply(Buffer.concat(chunks).toString("utf8")) : {})`,
		"const reply = (s) => { try { const r = JSON.parse(s); return r !== null && typeof r === \"object\" ? r : {} } catch { return {} } }",
		"} catch { end({}) }",
		`stdio: ["pipe", "pipe", "ignore"]`,
		`JSON.stringify({ type: "context", cwd: dir, sessionID })`,
		"const r = await ask(cwd, sessionID)",
		"parts.push({ id: partID(parts), messageID: message.id, sessionID: str(message.sessionID) ?? sessionID, type: \"text\", text: note, synthetic: true })",
		"p.pin = str(r.pin)",
		"p.pin_plan = str(r.pin_plan)",
	} {
		if n := strings.Count(src, want); n != 1 {
			t.Errorf("%d times, want once: %s", n, want)
		}
	}
	if n := strings.Count(src, "await "); n != 1 {
		t.Errorf("only chat.message waits, for the note: %d awaits", n)
	}
	if regexp.MustCompile(`output[\w.?]*\s*=[^=]`).MatchString(src) {
		t.Error("the plugin never assigns to output: OpenCode keeps its own parts array")
	}
	for _, hook := range []string{"event: async ({ event }) => {", `"chat.message": async (inp, output) => {`} {
		i := strings.Index(src, hook)
		if i < 0 {
			t.Fatalf("no %s", hook)
		}
		body := src[i+len(hook):]
		body = body[:strings.Index(body, "\n    },\n")]
		if !strings.HasPrefix(body, "\n      try {\n") || !strings.HasSuffix(body, "\n      } catch {}") {
			t.Errorf("%s is not one try with an empty catch:%s", hook, body)
		}
	}
}

// Rex knows its plugin by the first line, and which version it is: one
// from before the versions feeds the feed but never asks for the pin, so
// it is Old, and installing again replaces it (docs/ux.md §7a).
func TestOpenCodePluginVersions(t *testing.T) {
	for in, want := range map[string]int{
		opencodeMarker + "\nx":                                       opencodeVersion,
		"// generated by rex — safe to delete\nimport x":             1,
		"// generated by rex — safe to delete\r\nimport x":           1,
		"// generated by rex · plugin v3 — safe to delete\n":         3,
		"// generated by rex · plugin v1 — safe to delete\n":         0,
		"// generated by rex · plugin vx — safe to delete\n":         0,
		"// generated by rex — safe to delete, or not\n":             0,
		"// my own rex plugin\n// generated by rex — safe to delete": 0,
		"": 0,
	} {
		if got := pluginVersion([]byte(in)); got != want {
			t.Errorf("%q: v%d, want v%d", in, got, want)
		}
	}

	home := openCodeHome(t)
	path := filepath.Join(home, ".config", "opencode", "plugins", "rex.js")
	os.MkdirAll(filepath.Dir(path), 0o755)
	old := "// generated by rex — safe to delete\n// Pipes OpenCode events to rex, which only watches\nexport const RexPlugin = async () => ({})\n"
	os.WriteFile(path, []byte(old), 0o644)
	h := harnesses["opencode"]
	if !h.Wired(home) || !h.Old(home) {
		t.Fatal("an unversioned plugin is wired, and old")
	}
	var out bytes.Buffer
	if err := InstallHarness("opencode", &out); err != nil || !strings.Contains(out.String(), "installed opencode plugin at") || !strings.Contains(out.String(), "restart OpenCode") {
		t.Fatalf("%v %q", err, out.String())
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, opencodeSource(rexExecutable())) || !h.Wired(home) || h.Old(home) {
		t.Fatalf("installing again replaces it:\n%s", data)
	}
	os.WriteFile(path, []byte(old), 0o644)
	if err := UninstallHarness("opencode", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || h.Wired(home) || h.Old(home) {
		t.Fatal("and uninstalling removes it")
	}
	newer := []byte("// generated by rex · plugin v3 — safe to delete\nexport const RexPlugin = async () => ({})\n")
	os.WriteFile(path, newer, 0o644)
	if !h.Wired(home) || h.Old(home) {
		t.Fatal("a newer Rex's plugin is not old")
	}
	out.Reset()
	if err := InstallHarness("opencode", &out); err != nil || !strings.Contains(out.String(), "kept the opencode plugin at") || !strings.Contains(out.String(), "plugin v3; this Rex has v2") {
		t.Fatalf("%v %q", err, out.String())
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, newer) {
		t.Fatalf("installing over a newer Rex's plugin keeps it:\n%s", data)
	}
}

// The notes the stub rex replies with.
const (
	stubNote = "Rex note, as of this prompt: the task pinned in the person's plan, read just now from rex/plans/cuj/sendoff-demo.rex.json. The plan file is the source of truth; earlier Rex notes in this conversation are out of date.\n" +
		"Pinned task: card-highlight · Selected plan card is one block (status active, in plan \"Send-off demo\")\n" +
		"Does: ▌ on all three card rows\n" +
		"The person wants this turn to serve this task and stay inside its scope. If the prompt asks for something outside it, they expect you to say so before editing."
	stubEmpty = "Rex note: the person has rex/plans/team/auth.rex.json open in Rex; it has no tasks yet."
)

// stubRex is the rex TestOpenCodePluginAddsThePin's plugin runs: this
// test binary, which TestMain runs as it while REX_TEST_STUB_DIR is set.
// It logs each payload there, a context request in asked.jsonl and every
// other in sent.jsonl, and answers a request as $STUB says, in rex's own
// reply shape.
func stubRex() int {
	in, _ := io.ReadAll(os.Stdin)
	ask := bytes.Contains(in, []byte(`"type":"context"`))
	log := "sent.jsonl"
	if ask {
		log = "asked.jsonl"
	}
	if f, err := os.OpenFile(filepath.Join(os.Getenv("REX_TEST_STUB_DIR"), log), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Write(append(bytes.TrimSpace(in), '\n'))
		f.Close()
	}
	if !ask {
		return 0
	}
	card := map[string]any{"pin": "card-highlight", "pin_plan": "rex/plans/cuj/sendoff-demo.rex.json"}
	switch os.Getenv("STUB") {
	case "reply":
		os.Stdout.Write(ContextReply("opencode", "context", stubNote, card))
	case "empty":
		os.Stdout.Write(ContextReply("opencode", "context", stubEmpty, map[string]any{"pin_plan": "rex/plans/team/auth.rex.json"}))
	case "nameless":
		os.Stdout.Write(ContextReply("opencode", "context", stubNote, nil))
	case "plain":
		os.Stdout.WriteString(stubNote + "\n")
	case "hang":
		time.Sleep(20 * time.Second)
	case "fail":
		os.Stdout.Write(ContextReply("opencode", "context", stubNote, card))
		return 3
	case "big":
		os.Stdout.Write(bytes.Repeat([]byte("x"), openCodeCap+800))
	}
	return 0
}

// The plugin's chat.message under node, against a stub rex (docs/ux.md
// §7a), on every OS. A reply becomes one synthetic text part pushed onto
// the prompt's own parts, with an id that sorts after theirs (one at the
// last id included), and the prompt's event carries the task and plan the
// reply named, and only when it added the note. A rex that hangs costs
// the prompt 1.5 s and no more; one that replies, says nothing, says
// something that isn't its reply, fails, says too much or isn't there
// costs it nothing; a throw goes nowhere. A child session's prompt and
// one with no text of the person's never ask.
func TestOpenCodePluginAddsThePin(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("needs node")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A -race build takes about a second to start on macOS, which leaves
	// the stub no room under 1.5 s: there the plugin gets 5 s, and the
	// clock isn't judged.
	var start time.Duration
	for i := 0; i < 2; i++ {
		warm := exec.Command(self)
		warm.Env = append(os.Environ(), "REX_TEST_STUB_DIR="+t.TempDir())
		t0 := time.Now()
		if out, err := warm.CombinedOutput(); err != nil {
			t.Fatalf("the stub: %v\n%s", err, out)
		}
		start = time.Since(t0)
	}
	src, slow := opencodeSource(self), start > 300*time.Millisecond
	if slow {
		t.Logf("the stub takes %v to start, so the plugin waits 5 s and the clock isn't judged", start)
		src = bytes.Replace(src, []byte("const WAIT = 1500"), []byte("const WAIT = 5000"), 1)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "rex.mjs"), src, 0o644)
	os.WriteFile(filepath.Join(dir, "gone.mjs"), opencodeSource(filepath.Join(dir, "no-such-rex")), 0o644)
	driver := `import { readFileSync } from "node:fs"
import { RexPlugin } from "./rex.mjs"
import { RexPlugin as Gone } from "./gone.mjs"
const h = await RexPlugin({ directory: "/repo" }), g = await Gone({ directory: "/repo" })
await h.event({ event: { type: "session.created", properties: { info: { id: "child", parentID: "parent" } } } })
const out = {}
const user = (text) => [{ id: "prt_000000000001AAAAAAAAAAAAAA", type: "text", text }, { id: "prt_fffffffff000zzzzzzzzzzzzzz", type: "file", url: "file:///a.png" }]
async function run(name, mode, { hooks = h, sessionID = "parent", parts = user(name), message = { id: "msg_1", sessionID } } = {}) {
  process.env.STUB = mode
  const output = { message, parts }
  const t0 = Date.now()
  await hooks["chat.message"]({ sessionID }, output)
  out[name] = { ms: Date.now() - t0, same: output.parts === parts, parts: [...parts] }
}
await run("reply", "reply")
await run("empty", "empty", { parts: [{ id: "prt_000000000001AAAAAAAAAAAAAA", type: "text", text: "empty" }] })
await run("nameless", "nameless")
await run("top", "reply", { parts: [{ id: "prt_ffffffffffffAAAAAAAAAAAAAA", type: "text", text: "top" }] })
await run("silent", "silent")
await run("plain", "plain")
await run("hang", "hang")
await run("fail", "fail")
await run("big", "big")
await run("gone", "reply", { hooks: g })
await run("child", "reply", { sessionID: "child" })
await run("synthetic", "reply", { parts: [{ type: "text", text: "a background task's result", synthetic: true }, { type: "text", text: "x", ignored: true }] })
await run("nomessage", "reply", { message: null })
await run("frozen", "reply", { parts: Object.freeze(user("frozen")) })
await h["chat.message"](undefined, undefined)
await h["chat.message"]({ sessionID: "parent" }, { parts: "not a list" })
for (let i = 0; i < 600; i++) {
  let n = 0
  try { n = readFileSync(process.env.REX_TEST_STUB_DIR + "/sent.jsonl", "utf8").trim().split("\n").length } catch {}
  if (n >= 11) break
  await new Promise((r) => setTimeout(r, 50))
}
console.log(JSON.stringify(out))
`
	os.WriteFile(filepath.Join(dir, "driver.mjs"), []byte(driver), 0o644)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, filepath.Join(dir, "driver.mjs"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "REX_TEST_STUB_DIR="+dir)
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, b)
	}
	var got map[string]struct {
		MS    int
		Same  bool
		Parts []map[string]any
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("%v\n%s", err, b)
	}
	id := regexp.MustCompile(`^prt_[0-9a-f]{12}[0-9A-Za-z]{14}$`)
	added := map[string]string{"reply": stubNote, "empty": stubEmpty, "nameless": stubNote, "top": stubNote}
	for name, r := range got {
		if !r.Same {
			t.Errorf("%s: output.parts was replaced", name)
		}
		want := 2
		if name == "empty" || name == "top" {
			want = 1
		}
		if text, ok := added[name]; ok {
			last := r.Parts[len(r.Parts)-1]
			keys := make([]string, 0, len(last))
			for k := range last {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if len(r.Parts) != want+1 || fmt.Sprint(keys) != "[id messageID sessionID synthetic text type]" ||
				last["text"] != text || last["type"] != "text" || last["synthetic"] != true || last["messageID"] != "msg_1" || last["sessionID"] != "parent" {
				t.Errorf("%s: one synthetic text part, the note trimmed: %v", name, r.Parts)
				continue
			}
			pid, _ := last["id"].(string)
			for _, p := range r.Parts[:len(r.Parts)-1] {
				if !id.MatchString(pid) || pid <= p["id"].(string) {
					t.Errorf("%s: id %q, in OpenCode's shape and after %q", name, pid, p["id"])
				}
			}
			continue
		}
		if len(r.Parts) != want {
			t.Errorf("%s: a part was added: %v", name, r.Parts)
		}
	}
	// Wide bounds: a busy CI runner may spawn slowly, but only a rex that
	// hangs reaches the 1.5 s wait, and nothing outlasts it by much.
	if !slow {
		if r := got["hang"]; r.MS < 1400 || r.MS >= 4000 {
			t.Errorf("a rex that hangs holds the prompt %d ms, want 1.5 s", r.MS)
		}
		for name, r := range got {
			if name != "hang" && r.MS >= 1400 {
				t.Errorf("%s: held the prompt %d ms; only a rex that hangs may cost it time", name, r.MS)
			}
		}
	}

	asked, _ := os.ReadFile(filepath.Join(dir, "asked.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(asked)), "\n")
	if len(lines) != 11 {
		t.Fatalf("asked %d times, want the 11 root prompts with text (gone's rex isn't there):\n%s", len(lines), asked)
	}
	for _, l := range lines {
		if l != `{"type":"context","cwd":"/repo","sessionID":"parent"}` {
			t.Errorf("the request: %s", l)
		}
	}
	sent, _ := os.ReadFile(filepath.Join(dir, "sent.jsonl"))
	stamps := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(sent)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%v: %q", err, l)
		}
		for k := range m {
			switch k {
			case "type", "cwd", "sessionID", "text", "pin", "pin_plan":
			default:
				t.Errorf("not allow-listed: %s in %s", k, l)
			}
		}
		ev := MapOpenCode(m)
		if ev == nil || ev.Kind != "user" {
			t.Fatalf("%s: %+v", l, ev)
		}
		stamps[ev.Text] = fmt.Sprint(SentPin(m, "opencode"))
	}
	card := "map[pin:card-highlight pin_plan:rex/plans/cuj/sendoff-demo.rex.json]"
	want := map[string]string{"reply": card, "top": card, "empty": "map[pin_plan:rex/plans/team/auth.rex.json]"}
	for _, name := range []string{"nameless", "silent", "plain", "hang", "fail", "big", "nomessage", "frozen"} {
		want[name] = "map[]"
	}
	if fmt.Sprint(stamps) != fmt.Sprint(want) {
		t.Fatalf("each prompt's event, and what it says it carried:\n got %q\nwant %q", stamps, want)
	}
}
