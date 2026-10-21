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
		{`C:\Users\Jane Doe\bin\rex.exe`, "windows", `& 'C:/Users/Jane Doe/bin/rex.exe' ingest --source cursor`},
		{`C:\Users\O'Neil\rex.exe`, "windows", `& 'C:/Users/O''Neil/rex.exe' ingest --source cursor`},
		{`C:\Users\$env\rex.exe`, "windows", `& 'C:/Users/$env/rex.exe' ingest --source cursor`},
	}
	for _, c := range cases {
		if got := ingestCommand("cursor", c.exe, c.goos); got != c.want {
			t.Fatalf("%s/%s: %q want %q", c.goos, c.exe, got, c.want)
		}
	}
	ours := "'/new/rex' ingest --source cursor"
	for _, cmd := range []string{
		`"/old/rex" ingest --source cursor`, `'/new/rex' ingest --source cursor`, `/usr/local/bin/rex ingest --source cursor`,
		`& "C:/Users/Jane Doe/bin/rex.exe" ingest --source cursor`, `C:/bin/rex.exe ingest --source cursor`,
		`'/home/o'\''brien/rex' ingest --source cursor`,
		`& 'C:/Users/Jane Doe/bin/rex.exe' ingest --source cursor`, `& 'C:/Users/O''Neil/rex.exe' ingest --source cursor`,
	} {
		if !isRexIngestCommand(cmd, ours, "cursor") {
			t.Errorf("should recognise %q", cmd)
		}
	}
	for _, cmd := range []string{
		`rex ingest --source cursor && say done`, `/opt/myrex ingest --source cursor`,
		`/bin/rex ingest --source claude`, `./guard.sh`, `bash -c "rex ingest --source cursor"`,
	} {
		if isRexIngestCommand(cmd, ours, "cursor") {
			t.Errorf("must not delete the user's %q", cmd)
		}
	}
	if !isRexIngestCommand("/tmp/x/rexbin ingest --source cursor", "/tmp/x/rexbin ingest --source cursor", "cursor") {
		t.Fatal("our exact command always matches, whatever the binary is called")
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

func TestReinstallIsIdempotentWhateverTheBinaryIsCalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	cmd := "'/opt/tools/REXBIN' ingest --source cursor"
	install := func() []byte {
		if _, err := UpdateJSONFile(path, []byte("{}"), func(d *jsonedit.Doc) ([]byte, error) { return mergeCursorHooks(d, cmd) }); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		return data
	}
	first := install()
	if second := install(); !bytes.Equal(first, second) {
		t.Fatalf("second install changed the file:\n%s\n---\n%s", first, second)
	}
	if bytes.Count(first, []byte("REXBIN")) != len(CursorHookEvents) {
		t.Fatalf("one entry per event:\n%s", first)
	}
}

func TestInstallKeepsUserFieldsAndForeignCommands(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	cmd := IngestCommand()
	theirs := `{"version":1,"hooks":{"stop":[{"command":` + strconvQuote(cmd) + `,"timeout":5,"matcher":"Stop"},{"command":"rex ingest --source cursor && say done"}]}}`
	os.WriteFile(path, []byte(theirs), 0o644)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	got := readHooks(t, path)
	stop := got["hooks"].(map[string]any)["stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("%#v", stop)
	}
	mine := stop[0].(map[string]any)
	if mine["matcher"] != "Stop" || mine["timeout"] != float64(cursorHookTimeout) {
		t.Fatalf("user field dropped or timeout not set: %#v", mine)
	}
	if stop[1].(map[string]any)["command"] != "rex ingest --source cursor && say done" {
		t.Fatal("a compound user command must be kept")
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestFreshInstallIsReadable(t *testing.T) {
	home := cursorHome(t)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".cursor", "hooks.json"))
	for _, line := range strings.Split(string(data), "\n") {
		if len(line) > 200 {
			t.Fatalf("one-line hooks object:\n%s", data)
		}
	}
}

func TestInstallTreatsAnEmptyFileAsMissing(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("  \n"), 0o644)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := readHooks(t, path); got["hooks"] == nil {
		t.Fatal("hooks missing")
	}
}

func TestInstallRefusesNonListHookAndBrokenSymlink(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"version":1,"hooks":{"stop":{"command":"say done"}}}`), 0o644)
	if err := InstallCursorHook(io.Discard); err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Fatalf("err %v", err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `{"command":"say done"}`) {
		t.Fatal("file must be untouched")
	}
	os.Remove(path)
	if err := os.Symlink(filepath.Join(home, "nowhere.json"), path); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := InstallCursorHook(io.Discard); err == nil || !strings.Contains(err.Error(), "broken symlink") {
		t.Fatalf("err %v", err)
	}
	if info, _ := os.Lstat(path); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced")
	}
}

func TestRetiredSingleRexEntryIsRemoved(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"version":1,"hooks":{"beforeShellExecution":{"command":"'/old/rex' ingest --source cursor"},"stop":[]}}`), 0o644)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, ok := readHooks(t, path)["hooks"].(map[string]any)["beforeShellExecution"]; ok {
		t.Fatal("retired rex entry should be gone")
	}
}

func TestUsersEmptyRetiredListIsKept(t *testing.T) {
	home := cursorHome(t)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"version":1,"hooks":{"beforeShellExecution":[]}}`), 0o644)
	if err := InstallCursorHook(io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, ok := readHooks(t, path)["hooks"].(map[string]any)["beforeShellExecution"]; !ok {
		t.Fatal("an empty list Rex never wrote to is the user's; leave it")
	}
}

func TestIdleTurnEndsAreNotDeduped(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	for i := 0; i < 2; i++ {
		ok, err := AppendEvent(feed, NewEvent("cursor", "idle", "turn completed", nil))
		if err != nil || !ok {
			t.Fatalf("turn end %d dropped: %v", i, err)
		}
	}
}

func TestInterruptedToolEndsTheTurn(t *testing.T) {
	ev := MapPayload(map[string]any{"hook_event_name": "postToolUseFailure", "tool_name": "Shell", "is_interrupt": true}, "cursor")
	if ev == nil || ev.Kind != "idle" {
		t.Fatalf("%#v", ev)
	}
}

func TestHarnessRegistryCursor(t *testing.T) {
	home := cursorHome(t)
	var out bytes.Buffer
	if err := InstallHarness("auto", &out); err != nil || !strings.Contains(out.String(), "no supported agent") {
		t.Fatalf("auto with nothing present: %v %q", err, out.String())
	}
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	statuses := HarnessStatuses()
	if len(statuses) == 0 || statuses[0].Name != "cursor" || !statuses[0].Present || statuses[0].Wired {
		t.Fatalf("%#v", statuses)
	}
	out.Reset()
	if err := InstallHarness("auto", &out); err != nil || !strings.Contains(out.String(), "installed cursor hooks") {
		t.Fatalf("%v %q", err, out.String())
	}
	if !HarnessStatuses()[0].Wired {
		t.Fatal("wired after install")
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(data, []byte(`"stop": [`), []byte(`"stop": [{"command": "say done"}, `), 1), 0o644)
	out.Reset()
	if err := UninstallHarness("cursor", &out); err != nil {
		t.Fatal(err)
	}
	if HarnessStatuses()[0].Wired {
		t.Fatal("still wired after uninstall")
	}
	got := readHooks(t, path)
	hooks := got["hooks"].(map[string]any)
	if stop := hooks["stop"].([]any); len(stop) != 1 || stop[0].(map[string]any)["command"] != "say done" {
		t.Fatalf("foreign hook lost: %#v", hooks["stop"])
	}
	if _, ok := hooks["preToolUse"]; ok {
		t.Fatal("lists Rex emptied are removed")
	}
	if err := InstallHarness("nope", &out); err == nil {
		t.Fatal("unknown harness")
	}
}

func TestCursorUninstallLeavesAnEmptyFileAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("  \n"), 0o644)
	var out bytes.Buffer
	if err := UninstallHarness("cursor", &out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "  \n" {
		t.Fatalf("an empty hooks.json was rewritten: %q", data)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Fatal("nothing changed, so no .bak")
	}
	if !strings.Contains(out.String(), "no hooks") {
		t.Fatalf("said %q", out.String())
	}
}

func TestWiredCountsOnlyEntriesHarnessesRun(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	cmd := ingestCommand("cursor", rexExecutable(), runtimeGOOS)
	single, _ := json.Marshal(map[string]any{"version": 1, "hooks": map[string]any{"stop": map[string]any{"command": cmd}}})
	os.WriteFile(path, single, 0o644)
	if harnesses["cursor"].Wired(home) {
		t.Fatal("an object where a list belongs is never run, so it isn't wired")
	}
	list, _ := json.Marshal(map[string]any{"version": 1, "hooks": map[string]any{"stop": []any{map[string]any{"command": cmd}}}})
	os.WriteFile(path, list, 0o644)
	if !harnesses["cursor"].Wired(home) {
		t.Fatal("a list entry is wired")
	}
}

func TestInstallAutoReturnsEachFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("{broken"), 0o644)
	var out bytes.Buffer
	err := InstallHarness("auto", &out)
	if err == nil || !strings.Contains(err.Error(), "cursor:") {
		t.Fatalf("auto returned %v", err)
	}
	if strings.Contains(out.String(), "broken") {
		t.Fatal("the error goes back to the caller (stderr), not to stdout")
	}
}
