package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/mascot"
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
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle",
	})
	line := ansi.ReplaceAllString(strings.Split(frame, "\n")[0], "")
	if !strings.Contains(line, "REX") {
		t.Fatalf("missing REX: %q", line)
	}
	if strings.Index(line, "REX") < mascot.StageSize {
		t.Fatalf("REX not right of mascot: %q", line)
	}
	if !strings.Contains(frame, "ctrl+c") {
		t.Fatal("missing ctrl+c")
	}
	if strings.Contains(frame, "q quit") {
		t.Fatal("unexpected q quit")
	}
	if !strings.Contains(frame, "Ready.") {
		t.Fatal("missing Ready")
	}
	if !strings.Contains(frame, "╭") {
		t.Fatal("missing input box")
	}
}

func TestComposeFrameShowsAgentEvents(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle",
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
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle",
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
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle", Log: events, Scroll: 0,
	})
	older := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle", Log: events, Scroll: 40,
	})
	if !strings.Contains(live, "block-29") {
		t.Fatal("live missing newest")
	}
	if !strings.Contains(older, "block-00") {
		t.Fatal("older missing oldest")
	}
	if !strings.Contains(older, "↑↓") || !strings.Contains(older, "ctrl+e bottom") {
		t.Fatal("missing scroll footer")
	}
}

func TestComposeFrameStaysTopAlignedWhenShort(t *testing.T) {
	frame := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 6, AnimationName: "idle",
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
		Enabled: true, Columns: 80, Lines: 24, AnimationName: "idle",
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
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle",
	})
	armed := tui.ComposeFrame("idle", "yellow", tui.ComposeOptions{
		Enabled: false, Columns: 80, Lines: 24, AnimationName: "idle", QuitPending: true,
	})
	if !strings.Contains(armed, "press ctrl+c again to exit") {
		t.Fatal("missing quit hint")
	}
	if strings.Contains(idle, "press ctrl+c again to exit") {
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
	var stdout bytes.Buffer
	if code := Main([]string{"start"}, bytes.NewReader(nil), &stdout, &bytes.Buffer{}); code != 0 || !strings.Contains(stdout.String(), "REX") {
		t.Fatalf("code=%d %q", code, stdout.String())
	}
}
