package watch

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	err := InstallHarness("cursor", io.Discard)
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
	if err := InstallHarness("cursor", io.Discard); err == nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err == nil || !strings.Contains(err.Error(), "not a list") {
		t.Fatalf("err %v", err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `{"command":"say done"}`) {
		t.Fatal("file must be untouched")
	}
	os.Remove(path)
	if err := os.Symlink(filepath.Join(home, "nowhere.json"), path); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := InstallHarness("cursor", io.Discard); err == nil || !strings.Contains(err.Error(), "broken symlink") {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	if err := InstallHarness("cursor", io.Discard); err != nil {
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
	cursorStatus := func() HarnessStatus {
		for _, s := range HarnessStatusesFor("") {
			if s.Name == "cursor" {
				return s
			}
		}
		t.Fatal("cursor not registered")
		return HarnessStatus{}
	}
	if s := cursorStatus(); !s.Present || s.Wired {
		t.Fatalf("%#v", s)
	}
	out.Reset()
	if err := InstallHarness("auto", &out); err != nil || !strings.Contains(out.String(), "installed cursor hooks") {
		t.Fatalf("%v %q", err, out.String())
	}
	if !cursorStatus().Wired {
		t.Fatal("wired after install")
	}
	path := filepath.Join(home, ".cursor", "hooks.json")
	data, _ := os.ReadFile(path)
	os.WriteFile(path, bytes.Replace(data, []byte(`"stop": [`), []byte(`"stop": [{"command": "say done"}, `), 1), 0o644)
	out.Reset()
	if err := UninstallHarness("cursor", &out); err != nil {
		t.Fatal(err)
	}
	if cursorStatus().Wired {
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

// cursorInstall runs Rex's Cursor installer, or its uninstaller, on the
// hooks.json at path with a fixed command, as `rex ingest --install
// cursor` and `--uninstall cursor` do.
func cursorInstall(t *testing.T, path string, install bool) []byte {
	t.Helper()
	cmd := "'/opt/rex/bin/rex' ingest --source cursor"
	edit := func(d *jsonedit.Doc) ([]byte, error) { return mergeCursorHooks(d, cmd) }
	if !install {
		edit = func(d *jsonedit.Doc) ([]byte, error) { return removeCursorHooks(d, cmd) }
	}
	if _, err := UpdateJSONFile(path, []byte("{\n  \"version\": 1,\n  \"hooks\": {}\n}\n"), edit); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A fresh hooks.json, byte for byte: every event Rex registers, sessionStart
// (the pin when a chat starts, docs/ux.md §7a) among them.
func TestCursorFreshInstallGolden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	entry := `[
      {
        "command": "'/opt/rex/bin/rex' ingest --source cursor",
        "timeout": 20
      }
    ]`
	want := "{\n  \"version\": 1,\n  \"hooks\": {\n" +
		"    \"afterAgentResponse\": " + entry + ",\n" +
		"    \"afterAgentThought\": " + entry + ",\n" +
		"    \"afterFileEdit\": " + entry + ",\n" +
		"    \"beforeSubmitPrompt\": " + entry + ",\n" +
		"    \"postToolUse\": " + entry + ",\n" +
		"    \"postToolUseFailure\": " + entry + ",\n" +
		"    \"preToolUse\": " + entry + ",\n" +
		"    \"sessionStart\": " + entry + ",\n" +
		"    \"stop\": " + entry + "\n" +
		"  }\n}\n"
	if got := cursorInstall(t, path, true); string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// An install from before sessionStart gains just that entry when
// `rex ingest --install cursor` runs again: every other byte stays, the
// old file is the .bak, and a third run changes nothing.
func TestCursorReinstallAddsSessionStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	var old []string
	for _, name := range CursorHookEvents {
		if name != "sessionStart" {
			old = append(old, name)
		}
	}
	if len(old) != len(CursorHookEvents)-1 {
		t.Fatalf("sessionStart is registered: %v", CursorHookEvents)
	}
	var b strings.Builder
	b.WriteString("{\n  \"version\": 1,\n  \"hooks\": {\n")
	for i, name := range old {
		fmt.Fprintf(&b, "    %q: [{\"command\": \"'/opt/rex/bin/rex' ingest --source cursor\", \"timeout\": 20}]", name)
		if name == "stop" {
			b.WriteString(`, "mine": [{"command": "./audit.sh"}]`)
		}
		if i < len(old)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  }\n}\n")
	before := b.String()
	os.WriteFile(path, []byte(before), 0o644)
	got := cursorInstall(t, path, true)
	want := strings.Replace(before, "]\n  }\n}\n", "],\n    \"sessionStart\": [\n      {\n        \"command\": \"'/opt/rex/bin/rex' ingest --source cursor\",\n        \"timeout\": 20\n      }\n    ]\n  }\n}\n", 1)
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != before {
		t.Fatalf(".bak is the file before:\n%s", bak)
	}
	if again := cursorInstall(t, path, true); !bytes.Equal(again, got) {
		t.Fatalf("a third install changed it:\n%s", again)
	}
}

// Installing into a user's own hooks.json, LF or CRLF, keeps their hooks
// and every byte Rex doesn't own, line endings included, and uninstalling
// gives back exactly the file they had. (A list Rex's entry leaves empty
// goes with it, even one they had left empty: Rex can't tell it from one
// it made, and an empty list runs nothing.)
func TestCursorInstallUninstallRoundTrip(t *testing.T) {
	theirs := `{
  "version": 1,
  "zzz": {"keep": "me"},
  "hooks": {
    "sessionStart": [
      {"command": "./warm-cache.sh", "timeout": 3}
    ],
    "beforeSubmitPrompt": [{"command": "./guard.sh"}],
    "afterShellExecution": [{"command": "./log.sh"}]
  }
}
`
	for name, src := range map[string]string{"lf": theirs, "crlf": strings.ReplaceAll(theirs, "\n", "\r\n")} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hooks.json")
			os.WriteFile(path, []byte(src), 0o644)
			got := cursorInstall(t, path, true)
			for _, keep := range []string{`"zzz": {"keep": "me"}`, `{"command": "./warm-cache.sh", "timeout": 3}`, `, {"command": "./guard.sh"}]`, `"afterShellExecution": [{"command": "./log.sh"}]`} {
				if !bytes.Contains(got, []byte(keep)) {
					t.Fatalf("lost %s:\n%s", keep, got)
				}
			}
			if name == "crlf" && bytes.Count(got, []byte("\n")) != bytes.Count(got, []byte("\r\n")) {
				t.Fatalf("a bare LF in a CRLF file:\n%q", got)
			}
			hooks := readHooks(t, path)["hooks"].(map[string]any)
			for _, event := range CursorHookEvents {
				list, _ := hooks[event].([]any)
				var rex int
				for _, e := range list {
					if strings.Contains(fmt.Sprint(e.(map[string]any)["command"]), "ingest --source cursor") {
						rex++
					}
				}
				if rex != 1 {
					t.Fatalf("%s: %d rex entries: %v", event, rex, list)
				}
			}
			for _, event := range []string{"sessionStart", "beforeSubmitPrompt"} {
				if list := hooks[event].([]any); len(list) != 2 || list[0].(map[string]any)["command"] != "'/opt/rex/bin/rex' ingest --source cursor" {
					t.Fatalf("Rex's %s entry comes first, theirs after it: %v", event, list)
				}
			}
			if bak, _ := os.ReadFile(path + ".bak"); string(bak) != src {
				t.Fatal("the .bak is their file")
			}
			if back := cursorInstall(t, path, false); string(back) != src {
				t.Fatalf("uninstall gave back:\n%q\nwant:\n%q", back, src)
			}
		})
	}
}

// Rex's sessionStart and beforeSubmitPrompt entries go first, so the
// person's own hooks there answer after Rex's (Cursor keeps the last
// answer for what it doesn't merge, docs/ux.md §7a): an install moves an
// older entry there, keeping fields they added, and every other event's
// entry stays where it is or goes last.
func TestCursorInstallPutsThePinHooksFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	src := `{
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [
      {"command": "./scan.sh"},
      {"command": "'/opt/rex/bin/rex' ingest --source cursor", "timeout": 20, "matcher": "x"}
    ],
    "stop": [{"command": "./audit.sh"}]
  }
}
`
	os.WriteFile(path, []byte(src), 0o644)
	got := cursorInstall(t, path, true)
	moved := `"beforeSubmitPrompt": [
      {
        "command": "'/opt/rex/bin/rex' ingest --source cursor",
        "matcher": "x",
        "timeout": 20
      },
      {"command": "./scan.sh"}
    ],`
	if !bytes.Contains(got, []byte(moved)) {
		t.Fatalf("Rex's prompt entry first, its matcher kept:\n%s", got)
	}
	hooks := readHooks(t, path)["hooks"].(map[string]any)
	if stop := hooks["stop"].([]any); len(stop) != 2 || stop[0].(map[string]any)["command"] != "./audit.sh" {
		t.Fatalf("stop keeps theirs first: %v", stop)
	}
	if start := hooks["sessionStart"].([]any); len(start) != 1 {
		t.Fatalf("sessionStart: %v", start)
	}
	if again := cursorInstall(t, path, true); !bytes.Equal(again, got) {
		t.Fatalf("a second install changed it:\n%s", again)
	}
	back := cursorInstall(t, path, false)
	if !bytes.Contains(back, []byte("\"beforeSubmitPrompt\": [\n      {\"command\": \"./scan.sh\"}\n    ],")) || bytes.Contains(back, []byte("rex")) {
		t.Fatalf("uninstall leaves theirs as it was:\n%s", back)
	}
}

// A hooks.json that doesn't parse, or whose sessionStart isn't a list, is
// refused and left as it was, with no .bak.
func TestCursorInstallRefusesWhatItCantSplice(t *testing.T) {
	for _, src := range []string{
		"{\"version\": 1, \"hooks\": {\"stop\": [],}}",
		"{\"version\": 1, \"hooks\": {\"sessionStart\": {\"command\": \"./mine.sh\"}}}\n",
	} {
		path := filepath.Join(t.TempDir(), "hooks.json")
		os.WriteFile(path, []byte(src), 0o644)
		cmd := "'/opt/rex/bin/rex' ingest --source cursor"
		if _, err := UpdateJSONFile(path, nil, func(d *jsonedit.Doc) ([]byte, error) { return mergeCursorHooks(d, cmd) }); err == nil {
			t.Fatalf("%s: no error", src)
		}
		if got, _ := os.ReadFile(path); string(got) != src {
			t.Fatalf("changed: %s", got)
		}
		if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
			t.Fatal("no .bak when nothing was written")
		}
	}
}

// Installing says the hook hands Cursor's agent the pin, and how.
func TestCursorInstallSaysTheHookAddsThePin(t *testing.T) {
	cursorHome(t)
	var out bytes.Buffer
	if err := InstallHarness("cursor", &out); err != nil || !strings.Contains(out.String(), "cursor: when a chat starts, and before each prompt on Cursor builds that take it, the hook also adds the task pinned in this repo's Rex plan") {
		t.Fatalf("%v %q", err, out.String())
	}
}
