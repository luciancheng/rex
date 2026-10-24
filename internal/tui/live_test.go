package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

func TestMarkdownUserPromptAlignment(t *testing.T) {
	view := BuildLogView([]watch.Event{{Source: "cursor", Kind: "user", Text: "format this please"}}, 36, viewOpt{tall: true})
	if view[0].Kind != "user" {
		t.Fatalf("kind %s", view[0].Kind)
	}
	for _, line := range view {
		if strings.Contains(line.Text, "──") || strings.TrimSpace(strings.ToLower(line.Text)) == "you" {
			t.Fatalf("unexpected label %q", line.Text)
		}
	}
	var body []string
	for _, line := range view {
		if line.Kind == "user" && strings.Contains(line.Text, "format this please") {
			body = append(body, line.Text)
		}
	}
	if len(body) == 0 {
		t.Fatal("missing body")
	}
	if !strings.HasSuffix(strings.TrimRight(body[0], " "), "format this please") {
		t.Fatalf("%q", body[0])
	}
	if !strings.HasPrefix(body[0], " ") {
		t.Fatal("not right aligned")
	}
}

func TestUserPromptBoxIsLeftAlignedInside(t *testing.T) {
	prompt := "one two three four five six seven eight nine ten eleven"
	width := 40
	view := BuildLogView([]watch.Event{{Source: "cursor", Kind: "user", Text: prompt}}, width, viewOpt{tall: true})
	var body []string
	for _, line := range view {
		if line.Kind == "user" && strings.TrimSpace(line.Text) != "" {
			body = append(body, line.Text)
		}
	}
	if len(body) <= 1 {
		t.Fatalf("expected wrap, got %#v", body)
	}
	half := width / 2
	starts := map[int]bool{}
	var start int
	for _, line := range body {
		n := len(line) - len(strings.TrimLeft(line, " "))
		starts[n] = true
		start = n
		if line[start] == ' ' {
			t.Fatalf("interior not left aligned %q", line)
		}
		trimmed := strings.TrimRight(line, " ")
		if len([]rune(trimmed))-start > half {
			t.Fatalf("wider than half %q", line)
		}
		if len([]rune(strings.TrimRight(line, " "))) > width {
			t.Fatalf("wider than pane")
		}
	}
	if len(starts) != 1 {
		t.Fatalf("ragged box %v", starts)
	}
	if start < width-half {
		t.Fatalf("not right aligned start=%d", start)
	}
}

// Rex has no plan actions: the only verbs are help and breakdance, as
// /words in the plan list (docs/ux.md §2).
func TestParseVerb(t *testing.T) {
	cases := map[string]string{
		"?":           "help",
		"/help":       "help",
		" /HELP ":     "help",
		"breakdance":  "breakdance",
		"/breakdance": "breakdance",
		"sendoff":     "",
		"s":           "",
		"pin a":       "",
		"p":           "",
		"done":        "",
		"d":           "",
		"brief":       "",
		"b":           "",
		"help me":     "",
		"idle":        "",
		"":            "",
	}
	for in, want := range cases {
		verb, ok := parseVerb(in)
		if verb != want || ok != (want != "") {
			t.Errorf("parseVerb(%q) = %q %v", in, verb, ok)
		}
	}
}

func TestAdvanceBreakdancePlaysOnce(t *testing.T) {
	frames := mascot.MustAnimation("breakdance").Frames
	index, hold := 0, frames[0].Hold
	steps := 0
	for {
		var done bool
		index, hold, done = advance(frames, index, hold, false)
		steps++
		if done {
			break
		}
		if steps > 10000 {
			t.Fatal("did not finish")
		}
	}
	if index != len(frames)-1 {
		t.Fatalf("ended on %d, want last frame", index)
	}
	_, _, looped := advance(frames, len(frames)-1, 1, true)
	if looped {
		t.Fatal("idle-style loop should wrap, not finish")
	}
}

func TestApplyTurnStartsThinkingOnHookPrompt(t *testing.T) {
	turn := ApplyTurn(TurnIdle, watch.Event{Source: "cursor", Kind: "user", Text: "hello"})
	if turn != TurnThinking {
		t.Fatalf("got %d", turn)
	}
	if AnimationForTurn(turn) != "typing" {
		t.Fatal("thinking should type")
	}
	turn = ApplyTurn(turn, watch.Event{Source: "cursor", Kind: "thought", Text: "hmm"})
	if turn != TurnThinking {
		t.Fatal("thought should stay thinking")
	}
	if AnimationForTurn(turn) != "typing" {
		t.Fatal("thought should keep typing")
	}
	turn = ApplyTurn(turn, watch.Event{Source: "cursor", Kind: "command", Text: "ls"})
	if turn != TurnRunning {
		t.Fatal("tool should be running")
	}
	if AnimationForTurn(turn) != "typing" {
		t.Fatal("running should type")
	}
	turn = ApplyTurn(turn, watch.Event{Source: "cursor", Kind: "result", Text: "ok"})
	if turn != TurnRunning {
		t.Fatal("result should not idle mid-turn")
	}
	turn = ApplyTurn(turn, watch.Event{Source: "cursor", Kind: "text", Text: "done"})
	if turn != TurnIdle {
		t.Fatal("reply should idle")
	}
	if AnimationForTurn(turn) != "idle" {
		t.Fatal("idle animation")
	}
}

func TestApplyTurnIgnoresRexsOwnLines(t *testing.T) {
	turn := ApplyTurn(TurnThinking, watch.NewEvent("rex", "plan", "card-highlight → done", nil))
	if turn != TurnThinking {
		t.Fatal("Rex's own lines never end the turn")
	}
	turn = ApplyTurn(TurnIdle, watch.NewEvent("rex", "drift", "soft README.md (next to plans.go)", nil))
	if turn != TurnIdle {
		t.Fatal("Rex's own lines never start one")
	}
}

func viewText(view []LogLine) string {
	var b strings.Builder
	for _, line := range view {
		b.WriteString(line.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestBuildLogViewKeepsRunReplyHidesDone(t *testing.T) {
	view := BuildLogView([]watch.Event{
		{Source: "cursor", Kind: "thought", Text: "Checking the sprite."},
		{Source: "cursor", Kind: "command", Text: "Write /tmp/a.go", Meta: map[string]any{"tool": "Write", "path": "/tmp/a.go"}},
		{Source: "cursor", Kind: "result", Text: "wrote /tmp/a.go", Meta: map[string]any{"tool": "Write", "path": "/tmp/a.go"}},
		{Source: "cursor", Kind: "command", Text: "Read /tmp/a.go", Meta: map[string]any{"tool": "Read", "path": "/tmp/a.go"}},
		{Source: "cursor", Kind: "text", Text: "First reply."},
		{Source: "cursor", Kind: "text", Text: "Second reply."},
	}, 48, viewOpt{tall: true})
	text := viewText(view)
	for _, needle := range []string{"thinking  Checking the sprite.", "$ Write /tmp/a.go", "reply ───", "First reply", "Second reply"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %q in\n%s", needle, text)
		}
	}
	if strings.Contains(text, "done") || strings.Contains(text, "wrote /tmp") {
		t.Fatalf("done spam still in\n%s", text)
	}
	if strings.Contains(text, "Read /tmp") {
		t.Fatalf("file look still in\n%s", text)
	}
	replies := strings.Count(text, "reply ───")
	if replies != 2 {
		t.Fatalf("want two reply blocks, got %d\n%s", replies, text)
	}
}

func TestExtendLogGrowsStreamingReply(t *testing.T) {
	var log []watch.Event
	ExtendLog(&log, []watch.Event{watch.NewEvent("cursor", "text", "Typing now means", nil)})
	added := ExtendLog(&log, []watch.Event{watch.NewEvent("cursor", "text", "Typing now means a file edit.", nil)})
	if len(log) != 1 || len(added) != 1 {
		t.Fatalf("log=%d added=%d", len(log), len(added))
	}
	if !strings.Contains(log[0].Text, "file edit") {
		t.Fatalf("%q", log[0].Text)
	}
}

func TestExtendLogKeepsThoughtsAndRepliesNotResults(t *testing.T) {
	var log []watch.Event
	added := ExtendLog(&log, []watch.Event{
		watch.NewEvent("cursor", "thought", "Checking the card.", nil),
		{Source: "cursor", Kind: "command", Text: "Read a.go", Meta: map[string]any{"tool": "Read", "path": "a.go"}},
		{Source: "cursor", Kind: "result", Text: "wrote a.go", Meta: map[string]any{"tool": "Write", "path": "a.go"}},
		watch.NewEvent("cursor", "text", "The bar should span the card.", nil),
		{Source: "cursor", Kind: "command", Text: "Write a.go", Meta: map[string]any{"tool": "Write", "path": "a.go"}},
	})
	if len(log) != 3 {
		t.Fatalf("log=%d added=%d", len(log), len(added))
	}
	if log[0].Kind != "thought" || log[1].Kind != "text" || log[2].Kind != "command" {
		t.Fatalf("%#v", log)
	}
}

func TestExtendLogDropsRepeatedThoughts(t *testing.T) {
	var log []watch.Event
	first := watch.NewEvent("cursor", "thought", "Looking at live.py", nil)
	added := ExtendLog(&log, []watch.Event{first, watch.NewEvent("cursor", "thought", "Looking at live.py", nil)})
	if len(added) != 1 || len(log) != 1 {
		t.Fatalf("added=%d len=%d", len(added), len(log))
	}
}

func TestPrintCardIncludesIdentity(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintCard("idle", "yellow", false, &buf); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	if !strings.Contains(text, "REX") || !strings.Contains(text, "idle") {
		t.Fatalf("%s", text)
	}
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	if mascot.ColorEnabledWriter(true) {
		t.Fatal("NO_COLOR should disable color")
	}
}

func TestUnknownPalette(t *testing.T) {
	_, err := mascot.PaletteFor("sparkle")
	if err == nil || !strings.Contains(err.Error(), "unknown color") {
		t.Fatalf("%v", err)
	}
}

func TestNoColorHasNoANSI(t *testing.T) {
	out, err := staticCard(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\033") {
		t.Fatal("ansi in no-color")
	}
	if len(strings.Split(out, "\n")) <= 4 {
		t.Fatal("too short")
	}
}

func TestColorUsesTruecolor(t *testing.T) {
	out, err := staticCard(true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\033[38;2;") {
		t.Fatal("missing truecolor")
	}
}

func TestIdlePNGMatchesSprite(t *testing.T) {
	root := findRoot(t)
	image, err := mascot.LoadPNG(root + "/assets/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 40 || image.Height != 40 {
		t.Fatalf("%dx%d", image.Width, image.Height)
	}
	palette := mascot.Palettes["yellow"]
	if len(mascot.IdleRows) != image.Height || len(mascot.IdleRows[0]) != image.Width {
		t.Fatal("size mismatch")
	}
	for y, row := range mascot.IdleRows {
		for x, ch := range row {
			pix := image.Pixels[y][x]
			r, g, b, a := pix[0], pix[1], pix[2], pix[3]
			if ch == '.' {
				if !((r == 255 && g == 255 && b == 255) || a < 16) {
					t.Fatalf("transparent mismatch %d,%d", x, y)
				}
				continue
			}
			role := mascot.PixelRoles[ch]
			want := palette[role]
			if want != (mascot.RGB{r, g, b}) {
				t.Fatalf("%d,%d %c %v != %v", x, y, ch, want, mascot.RGB{r, g, b})
			}
		}
	}
}

func findRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("no go.mod")
	return ""
}

func TestAppendOnlyFeedShowsOneGrowingReply(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	tail := watch.NewEventTail(feed, false)
	var log []watch.Event
	for _, text := range []string{"The bar", "The bar now spans", "The bar now spans all three rows."} {
		if _, err := watch.AppendEvent(feed, watch.NewEvent("cursor", "text", text, nil)); err != nil {
			t.Fatal(err)
		}
		ExtendLog(&log, tail.Poll())
	}
	view := viewText(BuildLogView(log, 48, viewOpt{tall: true}))
	if strings.Count(view, "reply ───") != 1 || !strings.Contains(view, "all three rows") {
		t.Fatalf("want one merged reply\n%s", view)
	}
}

func TestIdleHeaderNeverSaysRunning(t *testing.T) {
	turn := ApplyTurn(TurnRunning, watch.Event{Source: "cursor", Kind: "idle", Text: "turn completed"})
	if turn != TurnIdle {
		t.Fatal("stop ends the turn")
	}
	if got := statusForTurn(turn, "command"); got != "idle" {
		t.Fatalf("header says %q after the turn ended", got)
	}
	if got := statusForTurn(TurnIdle, "question"); got != "ask" {
		t.Fatalf("a pending question still shows: %q", got)
	}
	if ApplyTurn(TurnRunning, watch.Event{Source: "cursor", Kind: "error", Text: "Write failed", Meta: map[string]any{"tool": "Write"}}) != TurnRunning {
		t.Fatal("a failed tool call keeps the turn running")
	}
	if ApplyTurn(TurnRunning, watch.Event{Source: "cursor", Kind: "error", Text: "turn ended with an error"}) != TurnIdle {
		t.Fatal("a turn-level error ends the turn")
	}
}

// The header word follows the turn: a tool failing mid-turn keeps it
// running, a completed turn is idle again, and only a turn that ended in
// an error says error.
func TestHeaderWordAfterFailures(t *testing.T) {
	failed := watch.Event{Source: "cursor", Kind: "error", Text: "Write failed", Meta: map[string]any{"tool": "Write"}}
	if got := statusForTurn(ApplyTurn(TurnRunning, failed), "error"); got != "running" {
		t.Fatalf("mid-turn failure: %q", got)
	}
	if got := statusForTurn(TurnIdle, "idle"); got != "idle" {
		t.Fatalf("after a completed turn: %q", got)
	}
	ended := watch.Event{Source: "cursor", Kind: "error", Text: "turn ended with an error"}
	if got := statusForTurn(ApplyTurn(TurnRunning, ended), "error"); got != "error" {
		t.Fatalf("a turn that ended in an error: %q", got)
	}
}

// staticCard is the card rex start prints for TERM=dumb, pipes and
// --static: the idle sprite in the default palette, with its title.
func staticCard(enabled bool) (string, error) {
	var b strings.Builder
	err := PrintCard("idle", mascot.DefaultPalette, enabled, &b)
	return b.String(), err
}
