package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/tui"
	"github.com/luciancheng/rex/internal/watch"
)

var ansi = regexp.MustCompile(`\033\[[0-9;]*[A-Za-z]`)

func TestHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := Main([]string{"--help"}, bytes.NewReader(nil), &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	text := stdout.String()
	if !strings.Contains(text, "dinosaur") {
		t.Fatalf("missing dinosaur in help: %s", text)
	}
	if !strings.Contains(text, "rex start") {
		t.Fatalf("missing rex start in help")
	}
}

func TestNoCommandPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	code := Main(nil, bytes.NewReader(nil), &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout.String(), "rex start") {
		t.Fatalf("expected help")
	}
}

func TestStartStaticCard(t *testing.T) {
	var stdout bytes.Buffer
	code := Main([]string{"start", "--static", "--no-color"}, bytes.NewReader(nil), &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	text := stdout.String()
	if !strings.Contains(text, "REX") || !strings.Contains(text, "idle") {
		t.Fatalf("card missing identity: %s", text)
	}
}

func TestStartNoninteractivePrintsCard(t *testing.T) {
	var stdout bytes.Buffer
	code := Main([]string{"start", "--no-color"}, bytes.NewReader(nil), &stdout, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stdout.String(), "REX") {
		t.Fatalf("expected card")
	}
}

func TestUnknownCommandIsErrorButNotBlock(t *testing.T) {
	var stderr bytes.Buffer
	code := Main([]string{"demo"}, bytes.NewReader(nil), &bytes.Buffer{}, &stderr)
	if code != 1 { // 2 would read as "block" to an agent hook
		t.Fatalf("code=%d", code)
	}
}

func TestComposeFramePutsTextRightOfMascot(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24,
	})
	line := ansi.ReplaceAllString(strings.Split(frame, "\n")[0], "")
	if !strings.Contains(line, "REX") {
		t.Fatalf("missing REX: %q", line)
	}
	if strings.Index(line, "REX") < mascot.StageSize {
		t.Fatalf("REX not right of mascot: %q", line)
	}
	wide := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 120, Lines: 24,
	})
	if !strings.Contains(wide, "ctrl+c×2 quit") {
		t.Fatal("missing quit hint when the footer has room")
	}
	if strings.Contains(frame, "q quit") {
		t.Fatal("unexpected q quit")
	}
	if !strings.Contains(frame, "Ready.") {
		t.Fatal("missing Ready")
	}
	if strings.Contains(frame, "╭") {
		t.Fatal("the tape has no input box")
	}
}

func TestComposeFrameShowsAgentEvents(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24,
		Log: []watch.Event{
			{Source: "cursor", Kind: "thought", Text: "I should inspect sprites.py"},
			{Source: "cursor", Kind: "command", Text: "pytest -q"},
			{Source: "cursor", Kind: "question", Text: "run tests?"},
		},
		Status: "thinking",
	})
	for _, needle := range []string{
		"I should inspect sprites.py",
		"$ pytest -q",
		"? run tests?",
		"── thinking",
		"── run",
		"── ask",
	} {
		if !strings.Contains(frame, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
	header := ansi.ReplaceAllString(strings.Split(frame, "\n")[0], "")
	if !strings.Contains(header, "thinking") {
		t.Fatalf("status missing: %q", header)
	}
	if strings.Contains(frame, "Ready.") {
		t.Fatal("Ready should be gone")
	}
}

func TestComposeFrameFormatsMarkdownAndUserPrompt(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24,
		Log: []watch.Event{
			{Source: "cursor", Kind: "user", Text: "can we format this"},
			{Source: "cursor", Kind: "text", Text: "## Summary\n- first item\n- **second** item\n"},
		},
	})
	plain := ansi.ReplaceAllString(frame, "")
	if !strings.Contains(plain, "can we format this") {
		t.Fatal("missing user prompt")
	}
	if strings.Contains(plain, "you ──") {
		t.Fatal("unexpected YOU rule")
	}
	if !strings.Contains(plain, "Summary") || !strings.Contains(plain, "• first item") || !strings.Contains(plain, "second item") {
		t.Fatalf("markdown not formatted: %s", plain)
	}
	if strings.Contains(plain, "##") || strings.Contains(plain, "**") {
		t.Fatal("raw markdown leaked")
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "can we format this") && !strings.HasSuffix(strings.TrimRight(line, "\r"), "can we format this") {
			t.Fatalf("user prompt not right aligned: %q", line)
		}
	}
}

func TestComposeFrameScrollsOlderEvents(t *testing.T) {
	events := make([]watch.Event, 0, 30)
	for i := 0; i < 30; i++ {
		events = append(events, watch.Event{Source: "cursor", Kind: "thought", Text: fmt.Sprintf("block-%02d", i)})
	}
	live := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, Log: events, Scroll: 0,
	})
	older := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, Log: events, Scroll: 40,
	})
	if !strings.Contains(live, "block-29") {
		t.Fatal("live missing newest")
	}
	if !strings.Contains(older, "block-00") {
		t.Fatal("older missing oldest")
	}
	if !strings.Contains(older, "↑ · end live") {
		t.Fatal("missing scroll footer")
	}
}

func TestComposeFrameStaysTopAlignedWhenShort(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 6,
	})
	line := ansi.ReplaceAllString(strings.Split(frame, "\n")[0], "")
	if strings.Index(line, "REX") < mascot.StageSize {
		t.Fatalf("not top aligned: %q", line)
	}
	if len(strings.Split(frame, "\n")) > 7 {
		t.Fatalf("too many lines")
	}
}

func TestComposeFrameKeepsFullColoredSprite(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: true, Columns: 80, Lines: 24,
	})
	body := strings.Join(mascot.MustRenderRows(
		mascot.FitStageDefault(mascot.MustSprite("idle").Rows),
		mascot.MustPalette("yellow"),
		true,
	), "\n")
	if strings.Count(frame, "▀") != strings.Count(body, "▀") {
		t.Fatal("truncated upper half blocks")
	}
	if strings.Count(frame, "▄") != strings.Count(body, "▄") {
		t.Fatal("truncated lower half blocks")
	}
}

func TestComposeFrameDoubleCtrlCHint(t *testing.T) {
	idle := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24,
	})
	armed := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, QuitPending: true,
	})
	if !strings.Contains(armed, "ctrl+c again to quit") {
		t.Fatal("missing quit hint")
	}
	if strings.Contains(idle, "ctrl+c again to quit") {
		t.Fatal("idle should not hint quit")
	}
}

func TestIngestCLIAppends(t *testing.T) {
	dir := t.TempDir()
	feed := dir + "/session.jsonl"
	stdin := bytes.NewBufferString(`{"hook_event_name":"afterAgentThought","text":"from the hook"}`)
	code := Main([]string{"ingest", "--source", "cursor", "--events", feed}, stdin, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	data, err := os.ReadFile(feed)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "from the hook") {
		t.Fatalf("unexpected feed: %s", data)
	}
}

func TestIngestFailsOpenOnBadFlagsAndSource(t *testing.T) {
	feed := t.TempDir() + "/session.jsonl"
	payload := `{"hook_event_name":"beforeSubmitPrompt","prompt":"still recorded"}`
	for _, args := range [][]string{
		{"ingest", "--no-such-flag"},
		{"ingest", "--source", "opencode-next", "--events", feed},
		{"ingest", "--source"},
	} {
		var stdout, stderr bytes.Buffer
		code := Main(args, bytes.NewBufferString(payload), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("%v exited %d; hooks read 2 as block", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v wrote stdout %q; hooks feed stdout to the agent", args, stdout.String())
		}
	}
	data, err := os.ReadFile(feed)
	if err != nil || !strings.Contains(string(data), "still recorded") {
		t.Fatalf("unknown source should still ingest as auto: %v %s", err, data)
	}
}

func TestIngestSwallowsACrash(t *testing.T) {
	defer func(old func(io.Reader, string, string) error) { ingestFn = old }(ingestFn)
	ingestFn = func(io.Reader, string, string) error { panic("boom") }
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--source", "cursor"}, strings.NewReader("{}"), &stdout, &stderr); code != 0 || stdout.Len() != 0 {
		t.Fatalf("a panic must still exit 0 with no stdout: %d %q", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "rex: ingest crashed: boom") {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestIngestNeverWritesStdout(t *testing.T) {
	feed := t.TempDir() + "/session.jsonl"
	var stdout bytes.Buffer
	stdin := bytes.NewBufferString(`not json at all`)
	if code := Main([]string{"ingest", "--events", feed}, stdin, &stdout, &bytes.Buffer{}); code != 0 || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
}

// A feed that can't be written is reported on stderr; the hook still exits
// 0 with nothing on stdout.
func TestIngestReportsAnUnwritableFeed(t *testing.T) {
	dir := t.TempDir()
	blocker := dir + "/not-a-dir"
	os.WriteFile(blocker, []byte("x"), 0o600)
	var stdout, stderr bytes.Buffer
	payload := `{"hook_event_name":"beforeSubmitPrompt","prompt":"hi"}`
	code := Main([]string{"ingest", "--source", "cursor", "--events", blocker + "/feed.jsonl"}, strings.NewReader(payload), &stdout, &stderr)
	if code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "rex: ") {
		t.Fatalf("code %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}

func TestStaticStartDecision(t *testing.T) {
	cases := []struct {
		static, tty bool
		term        string
		want        bool
	}{
		{false, true, "xterm-256color", false},
		{false, true, "dumb", true}, // a real TTY that can't move the cursor
		{true, true, "xterm", true},
		{false, false, "xterm", true},
	}
	for _, c := range cases {
		if got := staticStart(c.static, c.tty, c.term); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestStartDumbTerminalPrintsCard(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("FORCE_COLOR", "1") // even when color is forced, a dumb terminal gets none
	var stdout bytes.Buffer
	if code := Main([]string{"start", "--static"}, bytes.NewReader(nil), &stdout, &bytes.Buffer{}); code != 0 || !strings.Contains(stdout.String(), "REX") {
		t.Fatalf("code=%d %q", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "\033") {
		t.Fatalf("TERM=dumb printed escapes: %q", stdout.String())
	}
}

func TestIngestInstallFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	hooks := filepath.Join(home, ".cursor", "hooks.json")
	os.MkdirAll(filepath.Dir(hooks), 0o755)
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append([]string{"ingest"}, args...), bytes.NewReader(nil), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, _, stderr := run("--install", "cursor"); code != 0 {
		t.Fatalf("code=%d %s", code, stderr)
	}
	if data, _ := os.ReadFile(hooks); !strings.Contains(string(data), "ingest --source cursor") {
		t.Fatalf("hooks.json has no rex hook:\n%s", data)
	}
	if code, _, _ := run("--install-cursor-hook"); code != 0 {
		t.Fatal("the old flag still works")
	}
	if code, _, stderr := run("--uninstall", "cursor"); code != 0 {
		t.Fatalf("uninstall %s", stderr)
	}
	if data, _ := os.ReadFile(hooks); strings.Contains(string(data), "ingest --source") {
		t.Fatalf("uninstall left a rex hook:\n%s", data)
	}
	if code, _, stderr := run("--install", "vim"); code != 1 || !strings.Contains(stderr, "cursor") || !strings.Contains(stderr, ", auto)") {
		t.Fatalf("an unknown harness is an install error (exit 1, never 2), listing the names: %d %q", code, stderr)
	}
	if code, _, _ := run("--install"); code != 1 {
		t.Fatal("--install with no name is an install error")
	}
	if code, _, _ := run("--install", "cursor", "--uninstall", "cursor"); code != 1 {
		t.Fatal("--install and --uninstall together are refused")
	}
	// auto keeps going past a broken config, then says it failed.
	os.WriteFile(hooks, []byte("{not json"), 0o644)
	if code, _, stderr := run("--install", "auto"); code != 1 || !strings.Contains(stderr, "cursor") {
		t.Fatalf("auto hid a failed install: %d %q", code, stderr)
	}
}

func TestInstallFlagsRefuseMixedOrEmptyNames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	for _, args := range [][]string{
		{"--install-cursor-hook", "--uninstall", "cursor"},
		{"--install="},
		{"--uninstall="},
	} {
		var stdout, stderr bytes.Buffer
		if code := Main(append([]string{"ingest"}, args...), bytes.NewReader(nil), &stdout, &stderr); code != 1 {
			t.Errorf("%v: code %d, want 1 (%q)", args, code, stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "hooks.json")); err == nil {
		t.Fatal("a refused command wrote hooks")
	}
}

func TestProjectInstallsAreClaudeOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	for _, name := range []string{"auto", "cursor"} {
		var stdout, stderr bytes.Buffer
		if code := Main([]string{"ingest", "--install", name, "--project"}, bytes.NewReader(nil), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "only; nothing changed") {
			t.Fatalf("%s --project: code=%d %q", name, code, stderr.String())
		}
	}
	for _, f := range []string{filepath.Join(home, ".cursor", "hooks.json"), filepath.Join(home, ".claude", "settings.json")} {
		if _, err := os.Stat(f); err == nil {
			t.Fatalf("--project fell through to the machine-wide %s", f)
		}
	}
}

func TestInstallSkillWithoutAHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 1 || strings.Count(stderr.String(), "no home folder") != 2 {
		t.Fatalf("no HOME and no CLAUDE_CONFIG_DIR is an error for both copies, never a cwd-relative folder: %d %q", code, stderr.String())
	}
	// With CLAUDE_CONFIG_DIR, Claude Code's copy still goes in; the
	// ~/.agents copy can't, so it says so and exits 1.
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	stdout.Reset()
	stderr.Reset()
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "no home folder for ~/.agents/skills") {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(cfg, "skills", "rex-plan", "SKILL.md")); err != nil || !strings.Contains(stdout.String(), "only Claude Code reads; Codex, Cursor and OpenCode read ~/.agents/skills") {
		t.Fatalf("%v %q", err, stdout.String())
	}
}

// The $CLAUDE_CONFIG_DIR note says where Claude Code's copy is, so it
// never shows when that copy wasn't written.
func TestInstallSkillNoteNeedsClaudesCopy(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	own := filepath.Join(cfg, "skills", "rex-plan", "SKILL.md")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("my own skill"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "Rex didn't write it") {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "$CLAUDE_CONFIG_DIR") {
		t.Fatalf("no copy in $CLAUDE_CONFIG_DIR to point at: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "rex-plan", "SKILL.md")); err != nil {
		t.Fatal("the ~/.agents copy still installs")
	}
	if got, _ := os.ReadFile(own); string(got) != "my own skill" {
		t.Fatal("a SKILL.md Rex didn't write is left alone")
	}
}

// With $CLAUDE_CONFIG_DIR set, Rex's copy left in ~/.claude/skills (Cursor
// and OpenCode still read it) is updated and removed with the others;
// a SKILL.md there that Rex didn't write is left alone, and isn't an
// error.
func TestInstallSkillUpdatesRexsCopyBesideClaudeConfigDir(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	old := filepath.Join(home, ".claude", "skills", "rex-plan", "SKILL.md")
	os.MkdirAll(filepath.Dir(old), 0o755)
	os.WriteFile(old, []byte("<!-- generated by rex -->\nThe person marks tasks done in Rex (d⏎).\n"), 0o644)
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if got, _ := os.ReadFile(old); !bytes.Equal(got, skill.RexPlan) {
		t.Fatal("Rex's old copy in ~/.claude/skills is brought up to date")
	}
	for _, f := range []string{filepath.Join(cfg, "skills", "rex-plan", "SKILL.md"), filepath.Join(home, ".agents", "skills", "rex-plan", "SKILL.md")} {
		if got, _ := os.ReadFile(f); !bytes.Equal(got, skill.RexPlan) {
			t.Fatalf("%s installed", f)
		}
	}
	if skill.Look(home, "") != skill.Current {
		t.Fatal("every copy is current")
	}
	if code := Main([]string{"ingest", "--uninstall-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Dir(old)); !os.IsNotExist(err) {
		t.Fatal("--uninstall-skill takes it out too")
	}

	os.MkdirAll(filepath.Dir(old), 0o755)
	os.WriteFile(old, []byte("my own skill"), 0o644)
	stderr.Reset()
	for _, flag := range []string{"--install-skill", "--uninstall-skill"} {
		if code := Main([]string{"ingest", flag}, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 {
			t.Fatalf("%s: %d %q", flag, code, stderr.String())
		}
		if got, _ := os.ReadFile(old); string(got) != "my own skill" {
			t.Fatalf("%s: a SKILL.md Rex didn't write is never touched", flag)
		}
	}
}

// A newer Rex's copy (a teammate's, committed with --project) is kept:
// the command says so and succeeds, since the agent still learns plans.
func TestInstallSkillKeepsANewerRexsCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	newer := []byte(strings.Replace(string(skill.RexPlan), "· skill v", "· skill v9", 1))
	if bytes.Equal(newer, skill.RexPlan) {
		t.Fatal("the skill's marker has a version")
	}
	for _, dir := range skill.Dirs(home, "", false) {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), newer, 0o644)
	}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 || stderr.Len() != 0 || strings.Count(stdout.String(), "a newer Rex wrote it") != 2 {
		t.Fatalf("%d %q %q", code, stdout.String(), stderr.String())
	}
	for _, dir := range skill.Dirs(home, "", false) {
		if got, _ := os.ReadFile(filepath.Join(dir, "SKILL.md")); !bytes.Equal(got, newer) {
			t.Fatalf("%s: a newer Rex's copy is never replaced", dir)
		}
	}
}

// docs/plan-format.md, Teaching an agent: the skill goes where every agent
// reads it, Claude Code's folder and the shared .agents one, for this user
// or this repo; uninstall takes both copies back out.
func TestInstallSkillCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	user := []string{filepath.Join(home, ".claude", "skills", "rex-plan", "SKILL.md"), filepath.Join(home, ".agents", "skills", "rex-plan", "SKILL.md")}
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	for _, f := range user {
		if _, err := os.Stat(f); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Count(stdout.String(), "installed the rex-plan skill at") != 2 {
		t.Fatalf("one line per copy: %q", stdout.String())
	}
	stdout.Reset()
	if code := Main([]string{"ingest", "--install-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 || strings.Count(stdout.String(), "already installed") != 2 {
		t.Fatalf("%d %q", code, stdout.String())
	}
	if code := Main([]string{"ingest", "--uninstall-skill"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	for _, f := range user {
		if _, err := os.Stat(filepath.Dir(f)); !os.IsNotExist(err) {
			t.Fatalf("%s: uninstall removes both copies", f)
		}
	}

	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	wd, _ := os.Getwd()
	os.Chdir(repo)
	defer os.Chdir(wd)
	project := []string{filepath.Join(repo, ".claude", "skills", "rex-plan", "SKILL.md"), filepath.Join(repo, ".agents", "skills", "rex-plan", "SKILL.md")}
	if code := Main([]string{"ingest", "--install-skill", "--project"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("%d %s", code, stderr.String())
	}
	for _, f := range project {
		if _, err := os.Stat(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Dir(user[1])); !os.IsNotExist(err) {
		t.Fatal("--project writes nothing under home")
	}
	// A SKILL.md that isn't Rex's, in one place, stays; the other copy
	// still installs, and the command says what failed.
	os.Remove(project[1])
	os.WriteFile(project[1], []byte("my own skill"), 0o644)
	os.Remove(project[0])
	stderr.Reset()
	if code := Main([]string{"ingest", "--install-skill", "--project"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "Rex didn't write it") {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if _, err := os.Stat(project[0]); err != nil {
		t.Fatal("the other copy still installs")
	}
	if got, _ := os.ReadFile(project[1]); string(got) != "my own skill" {
		t.Fatal("a SKILL.md Rex didn't write is left alone")
	}
	stderr.Reset()
	if code := Main([]string{"ingest", "--uninstall-skill", "--project"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "wasn't written by Rex") {
		t.Fatalf("%d %q", code, stderr.String())
	}
	if _, err := os.Stat(project[0]); !os.IsNotExist(err) {
		t.Fatal("Rex's copy is removed even when the other isn't Rex's")
	}
	if got, _ := os.ReadFile(project[1]); string(got) != "my own skill" {
		t.Fatal("uninstall leaves a SKILL.md Rex didn't write")
	}
}

func TestSkillFlagsGoOnTheirOwn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	for _, args := range [][]string{
		{"--install-skill", "--uninstall-skill"},
		{"--install-skill", "--install", "claude"},
		{"--uninstall-skill", "--uninstall", "claude"},
	} {
		var stdout, stderr bytes.Buffer
		if code := Main(append([]string{"ingest"}, args...), strings.NewReader(""), &stdout, &stderr); code != 1 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills")); err == nil {
		t.Fatal("a refused command installed nothing")
	}
	plain := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(plain)
	defer os.Chdir(wd)
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"ingest", "--install-skill", "--project"}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "needs a git repo") {
		t.Fatalf("outside git, --project refuses: %d %q", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(plain, ".claude")); err == nil {
		t.Fatal("nothing is written outside a repo")
	}
}

// The install scripts: the real module path (no .git suffix), go install
// errors shown, REX_REF fetched exactly, Cursor the only agent wired, the
// skill offered under its own heading, and both scripts in step.
func TestInstallScripts(t *testing.T) {
	sh, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ps, err := os.ReadFile(filepath.Join("..", "..", "scripts", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"install.sh": string(sh), "install.ps1": string(ps)} {
		want := []string{"REX_REF", "fetch -q --depth 1 origin", "FETCH_HEAD", "ingest --install-cursor-hook", "cd <your-repo>"}
		for _, h := range watch.Harnesses() {
			want = append(want, "rex ingest --install "+h.Name) // adding a harness touches the scripts
		}
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", name, w)
			}
		}
		module, rex := `MODULE="github.com/luciancheng/rex"`, `"$REX"`
		installs, runs := `"$MODULE/cmd/rex@`, 0
		if name == "install.ps1" {
			module, rex, installs = `$Module = "github.com/luciancheng/rex"`, "& $Rex", `"$Module/cmd/rex@`
		}
		if !strings.Contains(body, module) {
			t.Errorf("%s: the module path is %s", name, module)
		}
		for _, line := range strings.Split(body, "\n") {
			code := strings.TrimSpace(line)
			if strings.HasPrefix(code, "#") {
				continue
			}
			if strings.Contains(code, "go install") {
				if !strings.Contains(code, installs) || strings.Contains(code, "REPO_URL") || strings.Contains(code, "RepoUrl") {
					t.Errorf("%s: go install takes the module path, never the repo URL: %q", name, line)
				}
				if strings.Contains(code, "2>") || strings.Contains(code, "Out-Null") {
					t.Errorf("%s: go install errors are shown, not swallowed: %q", name, line)
				}
			}
			// An install on a code line is only ever printed for the human
			// (a hint, or say / Write-Host), or it is the Cursor hook.
			if strings.Contains(code, "ingest --install") && !strings.Contains(code, "--install-cursor-hook") &&
				!strings.Contains(code, `hints+=("`) && !strings.Contains(code, `$hints += "`) &&
				!strings.HasPrefix(code, "say ") && !strings.HasPrefix(code, "Write-Host ") {
				t.Errorf("%s: only Cursor is wired; other installs are printed: %q", name, line)
			}
			// Every run of the new binary: its version, or the Cursor hook
			// through the alias every binary knows. Nothing else is wired.
			if i := strings.Index(code, rex+" "); i >= 0 {
				runs++
				if args := code[i+len(rex)+1:]; !strings.HasPrefix(args, "--version") && !strings.HasPrefix(args, "ingest --install-cursor-hook") {
					t.Errorf("%s: only Cursor is wired: %q", name, line)
				}
			}
		}
		if runs != 2 {
			t.Errorf("%s: the script runs rex %d times; the guard expects --version and the Cursor hook", name, runs)
		}
		// The skill is no agent: it has its own heading, never a line under
		// "Other agents".
		head := strings.Index(body, `"Teach your agents Rex plans (opt-in):"`)
		if skill := strings.Index(body, `  rex ingest --install-skill `); head < 0 || skill < head || strings.Contains(body, `hints += "rex ingest --install-skill`) || strings.Contains(body, `hints+=("rex ingest --install-skill`) {
			t.Errorf("%s: the skill hint goes under its own heading", name)
		}
		for _, bad := range []string{"rex.git/cmd/rex", "clone --branch", "--install auto", "go build -o rex "} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: must not contain %q", name, bad)
			}
		}
	}
	if bash, err := exec.LookPath("bash"); err == nil && runtime.GOOS != "windows" {
		if out, err := exec.Command(bash, "-n", filepath.Join("..", "..", "scripts", "install.sh")).CombinedOutput(); err != nil {
			t.Fatalf("bash -n: %v\n%s", err, out)
		}
	}
	if pwsh, err := exec.LookPath("pwsh"); err == nil { // CI's runners have it
		ps1, _ := filepath.Abs(filepath.Join("..", "..", "scripts", "install.ps1"))
		parse := "$e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('" + ps1 + "', [ref]$null, [ref]$e); if ($e) { $e; exit 1 }"
		if out, err := exec.Command(pwsh, "-NoProfile", "-NonInteractive", "-Command", parse).CombinedOutput(); err != nil {
			t.Fatalf("install.ps1 doesn't parse: %v\n%s", err, out)
		}
	}
}
