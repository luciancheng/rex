package watch

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
		{`{"type":"message.part.updated","part":{"type":"text","text":"All done."}}`, "text", "All done."},
		{`{"type":"message.part.updated","part":{"type":"reasoning","text":"Checking the card."}}`, "thought", "Checking the card."},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"running","input":{"command":"go test ./..."}}}}`, "command", "go test ./..."},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"completed","input":{"command":"go test ./..."}}}}`, "result", "ran go test ./..."},
		{`{"type":"message.part.updated","cwd":"/repo","part":{"type":"tool","tool":"edit","state":{"status":"running","input":{"filePath":"internal/tui/live.go"}}}}`, "command", "edit /repo/internal/tui/live.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"write","state":{"status":"completed","input":{"filePath":"/repo/a.go"}}}}`, "result", "wrote /repo/a.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"apply_patch","state":{"status":"completed","input":{},"files":["/repo/a.go","/repo/b.go"]}}}`, "result", "wrote /repo/a.go"},
		{`{"type":"message.part.updated","part":{"type":"tool","tool":"bash","state":{"status":"error","input":{"command":"go vet"},"error":"exit 1\nmore"}}}`, "error", "bash failed · go vet: exit 1"},
		{`{"type":"session.idle","properties":{"sessionID":"s"}}`, "idle", "turn completed"},
		{`{"type":"session.error","properties":{"error":{"message":"rate limited"}}}`, "error", "session error: rate limited"},
		{`{"type":"permission.asked","properties":{"title":"Run rm -rf build?"}}`, "question", "permission: Run rm -rf build?"},
		{`{"type":"question.asked","properties":{"questions":[{"question":"Which plan?"}]}}`, "question", "Which plan?"},
	}
	for _, c := range cases {
		ev := openCodeMap(t, c.payload)
		if ev == nil || ev.Kind != c.kind || !strings.Contains(ev.Text, c.text) || ev.Source != "opencode" {
			t.Errorf("%s\n got %#v", c.payload, ev)
		}
	}
	patch := openCodeMap(t, cases[7].payload)
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
	for _, leak := range []string{"delete input.content", "delete input.oldString", "delete input.newString", "delete input.patchText"} {
		if !strings.Contains(string(data), leak) {
			t.Fatalf("the plugin drops file bodies: missing %q", leak)
		}
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
