package watch

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/jsonedit"
)

func cursorHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func readHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	return out
}

func TestInstallCursorHookFreshFile(t *testing.T) {
	home := cursorHome(t)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	got := readHooks(t, path)
	if got["version"] != float64(1) {
		t.Fatalf("version %v", got["version"])
	}
	hooks := got["hooks"].(map[string]any)
	for _, name := range CursorHookEvents {
		list, ok := hooks[name].([]any)
		if !ok || len(list) != 1 {
			t.Fatalf("%s: %#v", name, hooks[name])
		}
		if cmd := list[0].(map[string]any)["command"].(string); !strings.Contains(cmd, "ingest --source cursor") {
			t.Fatalf("%s command %q", name, cmd)
		}
	}
	if _, ok := hooks["beforeShellExecution"]; ok {
		t.Fatal("beforeShellExecution duplicates preToolUse")
	}
}

func TestInstallCursorHookKeepsTheUsersFile(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	theirs := `{
  "version": 1,
  "zzz_custom": {"keep": "me <exactly>"},
  "hooks": {
    "beforeShellExecution": [
      {"command": "./guard.sh", "timeout": 5},
      {"command": "\"/old/bin/rex\" ingest --source cursor", "timeout": 20}
    ],
    "stop": [{"command": "say done"}]
  }
}
`
	if err := os.WriteFile(path, []byte(theirs), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte(`"zzz_custom": {"keep": "me <exactly>"}`)) {
		t.Fatalf("foreign key not kept byte for byte:\n%s", data)
	}
	if !bytes.Contains(data, []byte(`{"command": "./guard.sh", "timeout": 5}`)) {
		t.Fatalf("foreign hook lost:\n%s", data)
	}
	if bytes.Contains(data, []byte("/old/bin/rex")) {
		t.Fatalf("stale rex entry left behind:\n%s", data)
	}
	got := readHooks(t, path)
	stop := got["hooks"].(map[string]any)["stop"].([]any)
	if len(stop) != 2 || stop[0].(map[string]any)["command"] != "say done" {
		t.Fatalf("stop hooks %#v", stop)
	}
	if bak, err := os.ReadFile(path + ".bak"); err != nil || string(bak) != theirs {
		t.Fatalf("backup missing or wrong: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o640 {
			t.Fatalf("mode changed to %o", info.Mode().Perm())
		}
	}
	before, _ := os.ReadFile(path)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("reinstall is not idempotent:\n%s\n---\n%s", before, after)
	}
}

func TestInstallCursorHookRefusesInvalidJSON(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	jsonc := "{\n  // my hooks\n  \"version\": 1,\n  \"hooks\": {\"stop\": [{\"command\": \"x\"}],},\n}\n"
	if err := os.WriteFile(path, []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	err := InstallCursorHook(io.Discard)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != jsonc {
		t.Fatal("an unparseable hooks.json must be left untouched")
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup when nothing was written")
	}
}

func TestInstallCursorHookRefusesNonObjectHooks(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"version":1,"hooks":[]}`), 0o644)
	if err := InstallCursorHook(io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}

func TestIngestCommandQuoting(t *testing.T) {
	cases := []struct{ exe, goos, want string }{
		{"/Users/me/.local/bin/rex", "darwin", "'/Users/me/.local/bin/rex' ingest --source cursor"},
		{"/home/o'brien/$HOME/rex", "linux", `'/home/o'\''brien/$HOME/rex' ingest --source cursor`},
		{`C:\Users\me\.local\bin\rex.exe`, "windows", "C:/Users/me/.local/bin/rex.exe ingest --source cursor"},
		{`C:\Users\Jane Doe\bin\rex.exe`, "windows", `"C:/Users/Jane Doe/bin/rex.exe" ingest --source cursor`},
	}
	for _, c := range cases {
		if got := ingestCommand("cursor", c.exe, c.goos); got != c.want {
			t.Fatalf("%s/%s: %q want %q", c.goos, c.exe, got, c.want)
		}
	}
	if !isRexIngestCommand(`"/old/rex" ingest --source cursor`, "cursor") || !isRexIngestCommand(`'/new/rex' ingest --source cursor`, "cursor") {
		t.Fatal("must recognise old and new rex commands")
	}
}

func TestUpdateJSONFileRefusesConcurrentWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"a": 1}`), 0o644)
	_, err := UpdateJSONFile(path, nil, func(d *jsonedit.Doc) ([]byte, error) {
		os.WriteFile(path, []byte(`{"a": 1, "b": "someone else"}`), 0o644)
		return d.Set(d.Root(), "c", 3)
	})
	if err != ErrConfigChanged {
		t.Fatalf("err %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != `{"a": 1, "b": "someone else"}` {
		t.Fatalf("clobbered the other writer: %s", data)
	}
}
