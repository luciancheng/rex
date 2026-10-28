package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func thoughts(texts ...string) []watch.Event {
	var evs []watch.Event
	for _, s := range texts {
		evs = append(evs, watch.Event{Source: "claude", Kind: "thought", Text: s})
	}
	return evs
}

func commands(texts ...string) []watch.Event {
	var evs []watch.Event
	for _, s := range texts {
		evs = append(evs, watch.Event{Source: "claude", Kind: "command", Text: s, Meta: map[string]any{"tool": "Bash", "command": s}})
	}
	return evs
}

// edits are file edits as Claude Code's PreToolUse reports them: "Edit
// <path>", the tool and the path in meta.
func edits(paths ...string) []watch.Event {
	var evs []watch.Event
	for _, p := range paths {
		evs = append(evs, watch.Event{Source: "claude", Kind: "command", Text: "Edit " + p, Meta: map[string]any{"tool": "Edit", "path": p}})
	}
	return evs
}

func viewRows(log []watch.Event, width int, opt viewOpt) []string {
	return strings.Split(strings.TrimSuffix(viewText(BuildLogView(log, width, opt)), "\n"), "\n")
}

// docs/ux.md §10: a run of thoughts is one muted row, "thinking ×N ·" and
// the latest, whatever the turn is doing; one thought has no ×1; under 30
// cells only the label. Nothing in it is bold.
func TestThoughtRunFolds(t *testing.T) {
	run := thoughts("one", "two", "three", "four", "Looking at the plans view first; the card border is drawn apart.")
	rows := viewRows(run, 38, viewOpt{tall: true})
	if len(rows) != 1 || rows[0] != "  thinking ×5 · Looking at the plans…" {
		t.Fatalf("%q", rows)
	}
	for _, w := range []int{38, 24} {
		if l := BuildLogView(run, w, viewOpt{tall: true})[0]; l.Kind != "thought" || len(l.Spans) > 0 {
			t.Fatalf("%d cells: one muted run, nothing bold: %+v", w, l)
		}
	}
	if one := viewRows(thoughts("Checking the sprite."), 38, viewOpt{tall: true}); one[0] != "  thinking · Checking the sprite." {
		t.Fatalf("one thought: %q", one)
	}
	for _, w := range []int{24, 29} {
		if narrow := viewRows(run, w, viewOpt{tall: true}); len(narrow) != 1 || narrow[0] != "  thinking ×5" {
			t.Fatalf("%d cells: %q", w, narrow)
		}
		if one := viewRows(thoughts("hmm"), w, viewOpt{tall: true}); one[0] != "  thinking" {
			t.Fatalf("%d cells, one thought: %q", w, one)
		}
	}
	// The latest is shown as prose: Markdown stripped, on one line, its
	// list items the stream's bullet.
	if md := viewRows(thoughts("**Plan:**\n- read `live.go`\n- fix it"), 60, viewOpt{tall: true}); md[0] != "  thinking · Plan: · read live.go · fix it" {
		t.Fatalf("markdown: %q", md)
	}
	// A long latest thought still takes one row; a command after the run
	// starts a block of its own.
	long := thoughts("a", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter.")
	if mixed := viewRows(append(long, commands("go test ./...")...), 38, viewOpt{tall: true}); len(mixed) != 2 || mixed[1] != "  $ go test ./..." {
		t.Fatalf("%q", mixed)
	}
}

// docs/ux.md §10: one muted row per step after the gutter: "$" only for a
// shell command, "edit <path>" for a file edit, any other tool as its
// adapter wrote it; over 5 in a row, all but the last 4 fold into "… +N
// earlier", so a fold hides at least two and saves a row.
func TestStepRun(t *testing.T) {
	six := append(edits("a.go", "b.go"), commands("go vet ./...")...)
	six = append(six, edits("c.go", "d.go")...)
	six = append(six, commands("go test ./internal/tui -run PlanCardSelection -count=1 -v")...)
	got := viewRows(six, 38, viewOpt{tall: true})
	want := []string{"  … +2 earlier", "  $ go vet ./...", "  edit c.go", "  edit d.go", "  $ go test ./internal/tui -run PlanC…"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if four := viewRows(six[:4], 38, viewOpt{tall: true}); len(four) != 4 || strings.Contains(strings.Join(four, ""), "earlier") {
		t.Fatalf("four fit: %q", four)
	}
	// Five show whole: "+1 earlier" would hide one and save no row.
	if five := viewRows(six[:5], 38, viewOpt{tall: true}); len(five) != 5 || five[0] != "  edit a.go" || strings.Contains(strings.Join(five, ""), "earlier") {
		t.Fatalf("five: %q", five)
	}
	for n := 1; n <= 12; n++ {
		run := viewRows(commands(strings.Split(strings.Repeat("x,", n), ",")[:n]...), 38, viewOpt{tall: true})
		if len(run) > 5 || strings.Contains(run[0], "+1 earlier") || n > 5 && run[0] != fmt.Sprintf("  … +%d earlier", n-4) {
			t.Fatalf("%d commands: %q", n, run)
		}
	}
	for _, l := range BuildLogView(six, 38, viewOpt{}) {
		if l.Kind != "command" || len(l.Spans) != 1 || l.Spans[0].Role != "" {
			t.Fatalf("a step is one muted run: %+v", l)
		}
	}
	// Each tool reads as its own step: a delete, an edit with no path (a
	// todo list), a fetch, a command no tool names; a path keeps its file
	// name as it clips.
	for _, tc := range []struct {
		ev   watch.Event
		want string
	}{
		// A delete is a change to a file like any other: a step doesn't
		// always say which (a Codex patch's reads Edit).
		{watch.Event{Kind: "command", Text: "delete_file internal/tui/old.go", Meta: map[string]any{"tool": "delete_file", "path": "/r/internal/tui/old.go"}}, "  edit internal/tui/old.go"},
		{watch.Event{Kind: "command", Text: "todo: ship the card", Meta: map[string]any{"tool": "TodoWrite"}}, "  todo: ship the card"},
		{watch.Event{Kind: "command", Text: "WebFetch https://x.dev/a", Meta: map[string]any{"tool": "WebFetch"}}, "  WebFetch https://x.dev/a"},
		{watch.Event{Kind: "command", Text: "pytest -q"}, "  $ pytest -q"},
		{watch.Event{Kind: "command", Text: "Write internal/tui/very/deep/folder/view.go", Meta: map[string]any{"tool": "Write", "path": "/r/internal/tui/very/deep/folder/view.go"}}, "  edit …/deep/folder/view.go"},
		{watch.Event{Kind: "command", Text: "Edit internal/x.go (+2 more)", Meta: map[string]any{"tool": "apply_patch", "path": "/r/internal/x.go", "paths": []any{"/r/internal/x.go"}}}, "  edit internal/x.go (+2 more)"},
	} {
		if got := viewRows([]watch.Event{tc.ev}, 30, viewOpt{}); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q: %q, want %q", tc.ev.Text, got, tc.want)
		}
	}
}

// docs/ux.md §10: folding is display only. A run of thoughts is one row and
// a long run of steps shows its last 4, but the feed keeps every line the
// hooks wrote, AGENT's log every event, and Help over AGENT says so; no
// key unfolds a run.
func TestFoldIsDisplayOnly(t *testing.T) {
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	var evs []watch.Event
	evs = append(evs, thoughts("one", "two", "three", "four", "five")...)
	evs = append(evs, commands("a", "b", "c", "d", "e", "f", "g")...)
	for _, ev := range evs {
		if _, err := watch.AppendEvent(feed, ev); err != nil {
			t.Fatal(err)
		}
	}
	a := testApp(t)
	press(a, "\t")
	a.ingest(watch.NewEventTail(feed, false).Poll())
	if rows := viewRows(a.log, 78, viewOpt{tall: true}); len(rows) != 6 || rows[0] != "  thinking ×5 · five" || rows[1] != "  … +3 earlier" {
		t.Fatalf("folded on screen: %q", rows)
	}
	if len(a.log) != len(evs) {
		t.Fatalf("AGENT keeps every event: %d of %d", len(a.log), len(evs))
	}
	if kept := watch.LastEvents(feed, 100); len(kept) != len(evs) || kept[0].Text != "one" {
		t.Fatalf("the feed keeps every line: %d", len(kept))
	}
	for _, key := range []string{"\r", "right", " ", "o"} {
		before := viewText(a.logView(78))
		if press(a, key); viewText(a.logView(78)) != before {
			t.Fatalf("%q unfolded a run", key)
		}
	}
	press(a, "?")
	if help := strings.Join(strings.Fields(viewText(a.helpBody(78))), " "); !strings.Contains(help, "folding is display only: the feed keeps every thought") {
		t.Fatalf("Help says so\n%s", help)
	}
}

// docs/ux.md §9a, §10: a drift tag sits at its row's end, a cell short of
// the edge: the row clips first, keeping an edit's file name, and the tag
// stays. A fold that hides a tagged step takes its worst tag.
func TestDriftTagsSitAtTheRowsEnd(t *testing.T) {
	tag := func(ev watch.Event, level plan.DriftLevel) watch.Event {
		ev.Meta = withMeta(ev.Meta, driftKey, driftTag(level))
		return ev
	}
	deep := edits("server/a/very/deep/path/to/some/session_handler_impl.go")[0]
	for _, tc := range []struct {
		level plan.DriftLevel
		width int
		want  string
	}{
		{plan.Hard, 38, "  edit …ion_handler_impl.go   off pin "},
		{plan.Soft, 38, "  edit …ion_handler_impl.go  off pin? "},
		{plan.Hard, 78, "  edit server/a/very/deep/path/to/some/session_handler_impl.go        off pin "},
		{plan.Hard, 100, "  edit server/a/very/deep/path/to/some/session_handler_impl.go" + strings.Repeat(" ", 100-1-62-7) + "off pin "},
		{plan.Soft, 24, "  edit …pl.go  off pin? "},
	} {
		if row := paintLine(BuildLogView([]watch.Event{tag(deep, tc.level)}, tc.width, viewOpt{})[0], tc.width, false); row != tc.want {
			t.Errorf("%s at %d:\n got %q\nwant %q", tc.level, tc.width, row, tc.want)
		}
	}
	run := append(edits("a.go", "b.go"), commands("c", "d", "e", "f")...)
	run[0], run[1] = tag(run[0], plan.Soft), tag(run[1], plan.Hard)
	if fold := viewRows(run, 38, viewOpt{})[0]; !strings.HasPrefix(fold, "  … +2 earlier") || !strings.HasSuffix(fold, " off pin") {
		t.Fatalf("the fold keeps the worst tag it hides: %q", fold)
	}
	run[1] = tag(edits("b.go")[0], plan.OnPin)
	if fold := viewRows(run, 38, viewOpt{})[0]; !strings.HasSuffix(fold, " off pin?") {
		t.Fatalf("%q", fold)
	}
}

// docs/ux.md §10: a failed tool call is one row, ✗ in the gutter and
// "<what> · <why>" muted after it; when it doesn't fit, the command clips
// first and the reason keeps at least half the row. The turn's own error
// is red over up to 3 rows.
func TestFailedToolKeepsReason(t *testing.T) {
	fail := func(text string, meta map[string]any) watch.Event {
		return watch.Event{Source: "claude", Kind: "error", Text: text, Meta: meta}
	}
	cmd := "go test ./internal/tui -run PlanCardSelection"
	for _, tc := range []struct {
		ev    watch.Event
		width int
		want  string
	}{
		{fail("Bash failed · "+cmd+": exit status 1", map[string]any{"tool": "Bash", "command": cmd}), 38, "✗ go test ./internal/… · exit status 1"},
		{fail("Bash failed · "+cmd+": exit status 1", map[string]any{"tool": "Bash", "command": cmd}), 78, "✗ go test ./internal/tui -run PlanCardSelection · exit status 1"},
		// A command with ": " in it still splits at the reason.
		{fail(`Bash failed · echo "a: b": exit status 2`, map[string]any{"tool": "Bash", "command": `echo "a: b"`}), 38, `✗ echo "a: b" · exit status 2`},
		{fail("Write failed · README.md: permission denied", map[string]any{"tool": "Write", "path": "/r/README.md"}), 38, "✗ README.md · permission denied"},
		{fail("Edit failed: old_string not found", map[string]any{"tool": "Edit"}), 38, "✗ Edit · old_string not found"},
		{fail("Bash failed", map[string]any{"tool": "Bash"}), 38, "✗ Bash"},
		// A long reason keeps half the row; the command clips first.
		{fail("Bash failed · make all: error: the build broke in a way that takes many words", map[string]any{"tool": "Bash", "command": "make all"}), 38, "✗ make all · error: the build broke i…"},
		{fail("Bash failed · "+cmd+": panic: runtime error: index out of range [3] with length 3", map[string]any{"tool": "Bash", "command": cmd}), 38, "✗ go test ./inte… · panic: runtime er…"},
		// A failed write names its file from the left, as a path reads.
		{fail("Write failed · internal/tui/very/deep/view.go: permission denied", map[string]any{"tool": "Write", "path": "/r/internal/tui/very/deep/view.go"}), 38, "✗ …/deep/view.go · permission denied"},
		{fail("apply_patch failed · internal/tui/session_handler_impl.go: Failed to find expected lines", map[string]any{"tool": "apply_patch", "path": "/r/internal/tui/session_handler_impl.go"}), 38, "✗ …andler_impl.go · Failed to find ex…"},
	} {
		view := BuildLogView([]watch.Event{tc.ev}, tc.width, viewOpt{tall: true})
		got := viewRows([]watch.Event{tc.ev}, tc.width, viewOpt{tall: true})
		if len(got) != 1 || got[0] != tc.want || cellLen(got[0]) > tc.width {
			t.Errorf("%q at %d:\n got %q\nwant %q", tc.ev.Text, tc.width, got, tc.want)
		}
		if sp := view[0].Spans; len(sp) != 2 || sp[0] != (Span{Text: "✗", Role: "error"}) || sp[1].Role != "muted" {
			t.Errorf("only the ✗ is red: %+v", sp)
		}
	}
	// The adapter's own lead ("turn failed: ", "session error: ") goes:
	// the row already says error.
	turn := viewRows([]watch.Event{fail("turn failed: the model hit its context limit; the turn ended and nothing more can be done in it today or tomorrow", nil)}, 38, viewOpt{tall: true})
	want := []string{"✗ error · the model hit its context", "  limit; the turn ended and nothing", "  more can be done in it today or tom…"}
	if strings.Join(turn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("turn error:\n%s", strings.Join(turn, "\n"))
	}
	for text, want := range map[string]string{
		"session error: rate limited": "✗ error · rate limited",
		"turn ended with an error":    "✗ error · turn ended with an error",
		"turn failed":                 "✗ error · turn failed",
		"session error":               "✗ error · session error",
	} {
		if short := BuildLogView([]watch.Event{fail(text, nil)}, 38, viewOpt{}); len(short) != 1 || short[0].Text != want || short[0].Kind != "error" || len(short[0].Spans) > 0 {
			t.Errorf("%q: %+v, want %q in red", text, short, want)
		}
	}
	for _, w := range []int{24, 30, 38, 50} {
		for _, row := range viewRows([]watch.Event{fail("Bash failed · "+cmd+": exit status 1", map[string]any{"tool": "Bash"})}, w, viewOpt{tall: true}) {
			if cellLen(row) > w {
				t.Fatalf("%d cells: %q is %d", w, row, cellLen(row))
			}
		}
	}
}

// docs/ux.md §10: a path outside the repo and home keeps the OS's own
// separators (on Windows, `\`), and clips at them as it does at "/", so its
// last folders stay whole.
func TestPathEllipsisCutsAtTheOSSeparator(t *testing.T) {
	sep := string(filepath.Separator)
	p := strings.Join([]string{"D:", "scratch", "some", "deep", "folder", "view_handler.go"}, sep)
	for width, want := range map[int]string{
		22: "…" + sep + "view_handler.go",
		24: "…" + sep + "folder" + sep + "view_handler.go",
		16: "view_handler.go",
	} {
		if got := pathEllipsis(p, width); got != want {
			t.Errorf("%d cells: %q, want %q", width, got, want)
		}
	}
	write := watch.Event{Source: "claude", Kind: "error", Text: "Write failed · " + p + ": permission denied", Meta: map[string]any{"tool": "Write", "path": p}}
	if got, want := viewRows([]watch.Event{write}, 44, viewOpt{tall: true}), "✗ …"+sep+"view_handler.go · permission denied"; len(got) != 1 || got[0] != want {
		t.Fatalf("a failed write: %q, want %q", got, want)
	}
}

// docs/ux.md §10, the spacing column: a blank row before a reply and an
// ask and around a prompt; thoughts, steps and both ✗ lines pack.
func TestBlockSpacing(t *testing.T) {
	log := []watch.Event{
		{Source: "claude", Kind: "text", Text: "Done."},
		{Source: "claude", Kind: "user", Text: "now fix the test"},
	}
	log = append(log, thoughts("The test still fails.")...)
	log = append(log, commands("go test ./...")...)
	log = append(log,
		watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go test ./...: exit status 1", Meta: map[string]any{"tool": "Bash", "command": "go test ./..."}},
		watch.Event{Source: "claude", Kind: "error", Text: "turn failed: rate limited"},
		watch.Event{Source: "claude", Kind: "user", Text: "go on"},
		watch.Event{Source: "claude", Kind: "error", Text: "turn ended with an error"},
	)
	want := []string{
		"  Done.",
		"",
		"› now fix the test",
		"",
		"  thinking · The test still fails.",
		"  $ go test ./...",
		"✗ go test ./... · exit status 1",
		"✗ error · rate limited",
		"",
		"› go on",
		"",
		"✗ error · turn ended with an error",
	}
	if got := viewRows(log, 38, viewOpt{tall: true}); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// docs/ux.md §10: each reply is its own block of plain text after the
// gutter, no rule: list items hang under their text (the bullet a middle
// dot), headings bold, code muted "│ " lines.
func TestReplyBlocks(t *testing.T) {
	log := []watch.Event{
		{Source: "claude", Kind: "text", Text: "The selected card is one block now.\n- plans.go draws the card through one painter\n- live.go starts the fill at column 0"},
		{Source: "claude", Kind: "text", Text: "## Next\n\nRun it:\n\n```\ngo test ./...\n```"},
	}
	view := BuildLogView(log, 38, viewOpt{tall: true})
	want := []string{
		"  The selected card is one block now.",
		"  · plans.go draws the card through",
		"    one painter",
		"  · live.go starts the fill at column",
		"    0",
		"",
		"  Next",
		"",
		"  Run it:",
		"",
		"  │ go test ./...",
	}
	if got := strings.TrimSuffix(viewText(view), "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	for _, l := range view {
		switch strings.TrimSpace(l.Text) {
		case "Next":
			if len(l.Spans) < 2 || !l.Spans[1].Bold {
				t.Errorf("a heading is bold: %+v", l)
			}
		case "│ go test ./...":
			if l.Spans[0].Role != "muted" {
				t.Errorf("code is muted: %+v", l)
			}
		case "Run it:", "The selected card is one block now.":
			if l.Kind != "text" || l.Spans[1].Bold || l.Spans[1].Role != "" {
				t.Errorf("prose is plain text: %+v", l)
			}
		}
	}
	// Short screens drop the blank rows, inside a reply too.
	if short := viewText(BuildLogView(log, 38, viewOpt{})); strings.Contains(short, "\n\n") {
		t.Fatalf("blank rows under 12 rows\n%s", short)
	}
}

// docs/ux.md §10: Rex's own lines are muted "rex · …" after the gutter,
// nothing bold: a plan reload is one row, with no "(file)".
func TestRexLines(t *testing.T) {
	log := []watch.Event{watch.NewEvent("rex", "plan", "card-highlight → active · plan → active", nil)}
	view := BuildLogView(log, 38, viewOpt{tall: true})
	if got := viewText(view); got != "  rex · card-highlight → active · pla…\n" || view[0].Kind != "plan" || len(view[0].Spans) > 0 {
		t.Fatalf("%q %+v", got, view[0])
	}
	// From the app: a reload's line and an attach's.
	a, c, path := openDemo(t, demoPlan, 120, 32)
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)), 0o644)
	settled(a, c)
	tape := viewText(a.logView(78))
	if !strings.Contains(tape, "  rex · ") || strings.Contains(tape, "(file)") || strings.Contains(tape, "rex ›") {
		t.Fatalf("a reload's line\n%s", tape)
	}
}

// docs/ux.md §10: the stream has no rules, pills or old labels: every block
// is named by the gutter's mark or its first characters, and the warm
// start is a muted line with no blank row under it.
func TestNoRules(t *testing.T) {
	log := []watch.Event{watch.NewEvent("rex", "before", "before rex started", nil)}
	log = append(log, watch.Event{Source: "claude", Kind: "user", Text: "make it one block"})
	log = append(log, thoughts("hmm", "ok")...)
	log = append(log, commands("go test ./...")...)
	log = append(log,
		watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go test ./...: exit status 1", Meta: map[string]any{"tool": "Bash", "command": "go test ./..."}},
		watch.Event{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)?"},
		watch.Event{Source: "claude", Kind: "text", Text: "Done."},
		watch.Event{Source: "claude", Kind: "error", Text: "turn failed"},
		watch.NewEvent("rex", "plan", "card-highlight → done", nil),
		watch.NewEvent("rex", "drift", "", map[string]any{driftKey: driftTag(plan.Soft), "rel": "a.go", "verb": "edit"}),
	)
	for _, w := range []int{24, 38, 78} {
		v := BuildLogView(log, w, viewOpt{tall: true})
		for _, l := range v {
			for _, never := range []string{"──", "[ASK]", "reply", "rex ›", "▌", "•"} {
				if strings.Contains(l.Text, never) {
					t.Errorf("%d: %q in %q", w, never, l.Text)
				}
			}
			if cellLen(l.Text) > w {
				t.Errorf("%d: %q is %d cells", w, l.Text, cellLen(l.Text))
			}
		}
		if v[0].Text != "  before Rex started" || v[0].Kind != "before" || v[1].Kind == "gap" {
			t.Fatalf("%d: the warm start's line, no gap under it: %q", w, viewText(v))
		}
		// The gutter holds only the marks.
		for _, l := range v {
			if g := []rune(l.Text + "  ")[0]; g != ' ' && !strings.ContainsRune("›✗?", g) {
				t.Errorf("%d: %q in the gutter: %q", w, g, l.Text)
			}
		}
	}
}

// docs/ux.md §10: an ask is a bold ? in the gutter and the question,
// wrapped after the gutter, with a blank row before it only on tall
// screens.
func TestAskBlock(t *testing.T) {
	log := append(thoughts("a"), watch.Event{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./internal/tui ./internal/watch ./internal/cli -count=1)?"})
	tall := viewRows(log, 38, viewOpt{tall: true})
	at := -1
	for i, row := range tall {
		if strings.HasPrefix(row, "? Allow") {
			at = i
		}
	}
	if at < 1 || tall[at-1] != "" {
		t.Fatalf("no blank row before the ask: %q", tall)
	}
	if len(tall)-at < 3 {
		t.Fatalf("the question should wrap: %q", tall)
	}
	for _, row := range tall[at+1:] {
		if !strings.HasPrefix(row, "  ") || strings.HasPrefix(row, "   ") || cellLen(row) > 38 {
			t.Fatalf("not hung after the gutter: %q", tall)
		}
	}
	for _, row := range viewRows(log, 38, viewOpt{}) {
		if strings.TrimSpace(row) == "" {
			t.Fatalf("a blank row when short: %q", viewRows(log, 38, viewOpt{}))
		}
	}
	v := BuildLogView(log, 38, viewOpt{tall: true})
	if sp := v[at].Spans; len(sp) != 2 || sp[0] != (Span{Text: "? ", Role: "text", Bold: true}) || sp[1].Role != "text" || sp[1].Bold {
		t.Fatalf("a bold ? and the question in text: %+v", sp)
	}
}

// docs/ux.md §10: a prompt is › in the gutter and its text left-aligned
// after it, its later rows after the gutter; its paragraphs keep their
// blank rows only on tall screens; capped at 6 rows.
func TestPromptRows(t *testing.T) {
	text := "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen"
	rows := viewRows([]watch.Event{{Source: "cursor", Kind: "user", Text: text}}, 40, viewOpt{tall: true})
	if len(rows) < 3 || !strings.HasPrefix(rows[0], "› one two") {
		t.Fatalf("%q", rows)
	}
	for _, row := range rows[1:] {
		if !strings.HasPrefix(row, "  ") || strings.HasPrefix(row, "   ") || cellLen(row) > 40 {
			t.Fatalf("later rows start after the gutter: %q", rows)
		}
	}
	// The first row leaves a drift tag's room, so a tag never moves a row.
	if cellLen(rows[0]) > 40-tagRoom || cellLen(rows[1]) <= 40-tagRoom {
		t.Fatalf("the first row leaves the tag's room, the rest take the width: %q", rows)
	}
	v := BuildLogView([]watch.Event{{Source: "cursor", Kind: "user", Text: text}}, 40, viewOpt{tall: true})
	if sp := v[0].Spans; sp[0] != (Span{Text: "› ", Role: "muted"}) || sp[1].Role != "" || v[0].Kind != "user" {
		t.Fatalf("› muted, the prompt in text: %+v", sp)
	}
	blanks := func(opt viewOpt) int {
		n := 0
		for _, row := range viewRows([]watch.Event{{Source: "claude", Kind: "user", Text: "make it one block\n\nbar and fill"}}, 38, opt) {
			if strings.TrimSpace(row) == "" {
				n++
			}
		}
		return n
	}
	if blanks(viewOpt{}) != 0 || blanks(viewOpt{tall: true}) != 1 {
		t.Fatalf("blank rows: short %d, tall %d", blanks(viewOpt{}), blanks(viewOpt{tall: true}))
	}
	long := strings.Repeat("a line of a pasted wall of text\n", 12)
	capped := viewRows([]watch.Event{{Source: "claude", Kind: "user", Text: long}}, 38, viewOpt{})
	if len(capped) != maxPromptRows+1 || capped[maxPromptRows] != "  (+7 more rows)" {
		t.Fatalf("capped: %q", capped)
	}
}

// docs/ux.md §10, §12: under 12 rows AGENT has no blank rows (nor a page
// title), even around prompts and inside replies.
func TestNoBlankRowsWhenShort(t *testing.T) {
	// A short turn, so the prompt and its blank row are on screen at 7 rows.
	short := func(t *testing.T) *app {
		a, _, _ := cujApp(t)
		press(a, "\t")
		log := append([]watch.Event{{Source: "claude", Kind: "user", Text: "one block\n\nbar and fill"}}, thoughts("ok")...)
		for i := range log {
			log[i].TS = float64(i + 1)
		}
		a.ingest(log)
		return a
	}
	for name, a := range map[string]*app{"turn": turnApp(t), "prompt": short(t)} {
		for _, size := range [][2]int{{120, 11}, {80, 7}, {24, 7}, {80, 12}} {
			a.settle(size[0], size[1])
			rows := a.frameRows().Rows
			body := rows[2 : len(rows)-1]
			for len(body) > 0 && strings.TrimSpace(textOf(body[len(body)-1], size[0])) == "" {
				body = body[:len(body)-1] // filler under a short log is no gap
			}
			blank := 0
			for _, row := range body {
				if strings.TrimSpace(textOf(row, size[0])) == "" {
					blank++
				}
			}
			if size[1] < blankFrom && blank != 0 {
				t.Errorf("%s %dx%d: %d blank rows\n%s", name, size[0], size[1], blank, strings.Join(rows, "\n"))
			}
			if size[1] >= blankFrom && blank == 0 {
				t.Errorf("%s %dx%d: tall screens space blocks\n%s", name, size[0], size[1], strings.Join(rows, "\n"))
			}
			if name == "prompt" && size[0] >= 80 && !strings.Contains(strings.Join(rows, "\n"), "bar and fill") {
				t.Errorf("%dx%d: the prompt is off screen\n%s", size[0], size[1], strings.Join(rows, "\n"))
			}
		}
	}
}

// turnApp is on AGENT after a whole turn: a prompt, thoughts, steps, a
// failed step, an ask and a reply.
func turnApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := cujApp(t)
	press(a, "\t")
	log := []watch.Event{{Source: "claude", Kind: "user", Text: "make the selected card one block\n\nbar and fill on all three rows"}}
	log = append(log, thoughts("Looking at the plans view.", "The fill is per row.")...)
	log = append(log, edits("internal/tui/plans.go", "internal/tui/live.go")...)
	log = append(log, commands("go vet ./...")...)
	log = append(log, edits("internal/tui/plans_test.go")...)
	log = append(log, commands("go test ./internal/tui")...)
	log = append(log,
		watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go test ./internal/tui: exit status 1", Meta: map[string]any{"tool": "Bash", "command": "go test ./internal/tui"}},
		watch.Event{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)?"},
		watch.Event{Source: "claude", Kind: "text", Text: "The card is one block.\n\n- one painter\n- the fill starts at column 0"},
	)
	for i := range log {
		log[i].TS = float64(i + 1) // distinct, in order
	}
	a.ingest(log)
	return a
}

// docs/ux.md §10: the view is cached per (revision, width, tall): the
// turn's word never changes the layout, so a turn that ends drawing
// nothing (Cursor's stop) costs no layout, and a tag Rex puts on a row is
// a new revision.
func TestLogViewCache(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	press(a, "\t")
	a.ingest([]watch.Event{{Source: "cursor", Kind: "user", Text: "go", TS: 1}})
	a.ingest(thoughts("one", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell, which the card painter skips today."))
	if a.turn != TurnThinking {
		t.Fatalf("turn %v", a.turn)
	}
	w := PaneTextWidth(a.columns, a.lines)
	view, rev := a.logView(w), a.logRev
	if !a.ingest([]watch.Event{{Source: "cursor", Kind: "idle", Text: "turn completed", TS: 5}}) {
		t.Fatal("a turn end repaints (row 1's word)")
	}
	if a.logRev != rev || a.turn != TurnIdle || &a.logView(w)[0] != &view[0] {
		t.Fatalf("the turn's end lays nothing out again: rev %d→%d", rev, a.logRev)
	}
	a.ingest([]watch.Event{at(c, time.Second, edits(filepath.Join(a.plans.Repo, "README.md"))[0])})
	rows, rev := len(a.logView(w)), a.logRev
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "README.md")})
	if a.logRev == rev || len(a.logView(w)) != rows || !strings.HasSuffix(strings.TrimRight(viewText(a.logView(w)), "\n"), " off pin") {
		t.Fatalf("a tag is a new revision of the same rows: rev %d→%d, %d→%d rows\n%s", rev, a.logRev, rows, len(a.logView(w)), viewText(a.logView(w)))
	}
}

// The cap's trim is measured on the head of the stream alone (docs/ux.md
// §10): it must take as many rows as laying out the whole log twice says.
func TestTrimmedRows(t *testing.T) {
	kinds := []watch.Event{
		thoughts("one")[0],
		thoughts("The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell.")[0],
		commands("go test ./...")[0],
		edits("internal/tui/plans.go")[0],
		{Source: "claude", Kind: "command", Text: "Read internal/tui/live.go", Meta: map[string]any{"tool": "Read", "path": "internal/tui/live.go"}},
		{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": "README.md", "ok": true}},
		{Source: "claude", Kind: "user", Text: "make the selected card one block, with the bar and the fill on all three rows of it"},
		{Source: "claude", Kind: "text", Text: "## Done\n\nThe card is one block.\n\n- one painter\n\n```go\nfill(0)\n```"},
		{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)?"},
		{Source: "claude", Kind: "error", Text: "Bash failed · go vet: exit status 1", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "error", Text: "turn failed: the model hit its context limit"},
		watch.NewEvent("rex", "before", "before Rex started", nil),
		watch.NewEvent("rex", "plan", "following sendoff-demo", nil),
		watch.NewEvent("rex", "drift", "", map[string]any{driftKey: driftTag(plan.Hard), "rel": "README.md"}),
		{Source: "claude", Kind: "text", Text: "\x1b]0;title\x07"},
		{Source: "claude", Kind: "text", Text: "```\n```"},
		{Source: "claude", Kind: "idle", Text: "turn completed"},
	}
	// One filter with the feed's (watch.KeepInPane): a file look, a result,
	// a turn end and a reply that cleans to nothing draw nothing.
	for _, i := range []int{4, 5, len(kinds) - 3, len(kinds) - 2, len(kinds) - 1} {
		if !hiddenInStream(kinds[i]) {
			t.Fatalf("%q should draw nothing", kinds[i].Text)
		}
	}
	seed := uint32(1)
	next := func(n int) int { // a fixed sequence, so a failure replays
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	// A run of seven steps trimmed to five unfolds: the head must reach
	// past the whole run.
	run := commands("a", "b", "c", "d", "e", "f", "g")
	for round := 0; round <= 300; round++ {
		log := run
		if round > 0 {
			log = make([]watch.Event, 1+next(14))
			for i := range log {
				log[i] = kinds[next(len(kinds))]
			}
		}
		for _, width := range []int{20, 38, 80} {
			for _, opt := range []viewOpt{{}, {tall: true}} {
				for n := 0; n <= len(log)+1; n++ {
					want := len(BuildLogView(log, width, opt)) - len(BuildLogView(log[min(n, len(log)):], width, opt))
					if got := trimmedRows(log, n, width, opt); got != want {
						var ks []string
						for _, ev := range log {
							ks = append(ks, ev.Kind)
						}
						t.Fatalf("trim %d of %v at %d %+v: %d rows, want %d", n, ks, width, opt, got, want)
					}
				}
			}
		}
	}
}

// agentBody is AGENT's body rows as text, under the page title.
func agentBody(a *app) []string {
	rows := a.frameRows().Rows
	top, bottom := 2, len(rows)-1
	if a.lines >= blankFrom {
		top, bottom = 3, len(rows)-2 // row 2 is ↑ N earlier's, and the notice row the footer's
	}
	var out []string
	for _, r := range rows[top:bottom] {
		out = append(out, textOf(r, a.columns))
	}
	return out
}

// liveApp is on AGENT with a reply every 3 rows above a prompt and a run
// of thoughts, the turn in flight.
func liveApp(t *testing.T) *app {
	t.Helper()
	a := testApp(t)
	press(a, "\t")
	var log []watch.Event
	for i := 0; i < 30; i++ {
		log = append(log, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("reply %d", i), TS: float64(i)})
	}
	a.ingest(log)
	a.ingest(append([]watch.Event{{Source: "claude", Kind: "user", Text: "go on", TS: 40}}, thoughts("one", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell.")...))
	return a
}

// docs/ux.md §10: scrolling never changes the layout, and a scrolled view
// keeps its lines while the run grows, the turn ends, a block lands under
// it or the log trims; row 0 counts the rows under it and row 2 those
// above; End is live again.
func TestScrolledViewStable(t *testing.T) {
	for _, up := range []int{1, 2, 3} {
		a := liveApp(t)
		plain := a.logLines()
		for i := 0; i < up; i++ {
			press(a, "up")
		}
		if a.scroll != up || a.logLines() != plain {
			t.Fatalf("scrolled up, the layout stays: scroll %d, %d→%d lines", a.scroll, plain, a.logLines())
		}
		first := strings.Join(agentBody(a), "\n")
		for _, s := range []struct {
			what string
			evs  []watch.Event
		}{
			{"a thought joins the run", thoughts("and another")},
			{"the turn ends", []watch.Event{{Source: "claude", Kind: "idle", Text: "turn completed", TS: 50}}},
			{"a command", commands("go test ./...")},
			{"a reply", []watch.Event{{Source: "claude", Kind: "text", Text: "Done.", TS: 60}}},
		} {
			a.ingest(s.evs)
			if got := strings.Join(agentBody(a), "\n"); got != first {
				t.Fatalf("%d↑, %s moved the view:\n%s\n→\n%s", up, s.what, first, got)
			}
			rows := a.frameRows().Rows
			if got := textOf(rows[0], a.columns); !strings.Contains(got, fmt.Sprintf("↓ %d newer", a.scroll)) {
				t.Fatalf("%s: row 0 says what is below: %q (scroll %d)", s.what, got, a.scroll)
			}
			above := a.logLines() - PaneBodyHeight(a.lines) - a.scroll
			if got := strings.TrimSpace(textOf(rows[2], a.columns)); got != fmt.Sprintf("↑ %d earlier", above) {
				t.Fatalf("%s: row 2 says what is above: %q, want %d", s.what, got, above)
			}
		}
	}
	// The cap drops the oldest events off the top: the view stays.
	a := liveApp(t)
	top := func() string { return strings.Join(agentBody(a), "\n") }
	var more []watch.Event
	for i := 0; i < MaxLog; i++ {
		more = append(more, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("later %d", i), TS: float64(100 + i)})
	}
	a.ingest(more[:MaxLog-len(a.log)])
	press(a, "end")
	for i := 0; i < 40; i++ {
		press(a, "up")
	}
	first := top()
	a.ingest(more[MaxLog-30:])
	if got := top(); got != first || a.scroll == 0 {
		t.Fatalf("a trim moved the view: %q → %q (scroll %d)", first, got, a.scroll)
	}
	press(a, "end")
	if a.scroll != 0 {
		t.Fatalf("end is live: scroll %d", a.scroll)
	}
}

// docs/ux.md §10, §12: a reply that cleans to nothing (only an escape
// sequence, an empty fence) draws no block, so no blank row either: under
// 12 rows there are still none.
func TestEmptyReplyDrawsNothing(t *testing.T) {
	empty := []watch.Event{{Source: "claude", Kind: "text", Text: "\x1b]0;x\x07"}, {Source: "claude", Kind: "text", Text: "```\n```"}}
	if v := BuildLogView(empty, 38, viewOpt{tall: true}); len(v) != 0 {
		t.Fatalf("%q", viewText(v))
	}
	log := []watch.Event{{Source: "claude", Kind: "user", Text: "go", TS: 1}, empty[0]}
	log = append(log, thoughts("ok")...)
	log = append(log, empty[1])
	log = append(log, commands("x")...)
	if got := strings.Join(viewRows(log, 38, viewOpt{tall: true}), "\n"); got != "› go\n\n  thinking · ok\n  $ x" {
		t.Fatalf("%q", got)
	}
	a := testApp(t)
	press(a, "\t")
	for i := range log {
		log[i].TS = float64(i + 1)
	}
	a.ingest(log)
	a.settle(80, 10)
	body := agentBody(a)
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	if strings.Join(body, "\n") != "› go\n  thinking · ok\n  $ x" {
		t.Fatalf("no blank rows under 12 rows\n%s", strings.Join(body, "\n"))
	}
}

// docs/ux.md §10: a tool call that draws nothing (a file look between
// thoughts) leaves the stream still.
func TestHiddenReadKeepsTheStream(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	a.settle(80, 24)
	var log []watch.Event
	for i := 0; i < 20; i++ {
		cmd := fmt.Sprintf("go test %02d", i)
		log = append(log,
			watch.Event{Source: "cursor", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "shell"}, TS: float64(i)},
			watch.Event{Source: "cursor", Kind: "error", Text: "shell failed · " + cmd + ": exit status 1", Meta: map[string]any{"tool": "shell", "command": cmd}, TS: float64(i)})
	}
	a.ingest(log)
	long := "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell."
	for i := 0; i < 3; i++ {
		a.ingest([]watch.Event{{Source: "cursor", Kind: "thought", Text: long, TS: float64(50 + i)}})
		body, lines := strings.Join(agentBody(a), "\n"), a.logLines()
		path := filepath.Join(a.plans.Repo, "internal", "tui", fmt.Sprintf("f%d.go", i))
		a.ingest([]watch.Event{{Source: "cursor", Kind: "command", Text: "Read " + path, Meta: map[string]any{"tool": "Read", "path": path}, TS: float64(100 + i)}})
		if a.turn != TurnRunning {
			t.Fatalf("a read is the turn running: %v", a.turn)
		}
		if got := strings.Join(agentBody(a), "\n"); got != body || a.logLines() != lines {
			t.Fatalf("read %d moved the stream (%d→%d rows):\n%s\n→\n%s", i, lines, a.logLines(), body, got)
		}
	}
	// The next drawn block is a row of its own.
	rows := a.logLines()
	a.ingest(commands("go vet ./..."))
	if a.logLines() != rows+1 {
		t.Fatalf("a command under the run: %d→%d rows", rows, a.logLines())
	}
}

// docs/ux.md §10: scrolling never changes the layout, so one ↑ from the
// bottom moves the view one row, and ↓ moves it back.
func TestUpFromLiveMovesOneRow(t *testing.T) {
	a := liveApp(t)
	before := agentBody(a)
	press(a, "up")
	after := agentBody(a)
	for i := 0; i+1 < len(after); i++ {
		if after[i+1] != before[i] {
			t.Fatalf("one ↑ didn't move the view one row:\n%s\n→\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
		}
	}
	if press(a, "down"); strings.Join(agentBody(a), "\n") != strings.Join(before, "\n") {
		t.Fatalf("↓ didn't come back:\n%s", strings.Join(agentBody(a), "\n"))
	}
}

// docs/ux.md §10: when one row is all that overflows, ↑, Home and PgUp
// reach the oldest row, and ↑ N earlier says what is cut off the top:
// on row 2 from 12 rows, on the body's first row under 12.
func TestOldestRowReachable(t *testing.T) {
	for _, lines := range []int{32, 10} {
		a := testApp(t)
		press(a, "\t")
		a.settle(a.columns, lines)
		bodyH := PaneBodyHeight(a.lines)
		var log []watch.Event
		for i := 0; i <= bodyH; i++ {
			cmd := fmt.Sprintf("x%02d", i)
			log = append(log,
				watch.Event{Source: "claude", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "Bash"}, TS: float64(i)},
				watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · " + cmd + ": boom", Meta: map[string]any{"tool": "Bash", "command": cmd}, TS: float64(i)})
		}
		a.ingest(log)
		for a.logLines() > bodyH+1 {
			a.log = a.log[1:]
			a.logRev++
		}
		a.settle(a.columns, a.lines)
		oldest := strings.SplitN(viewText(a.logView(PaneTextWidth(a.columns, a.lines))), "\n", 2)[0]
		// Row 2 from 12 rows; under 12 it is the body's first row, which the
		// marker takes, so it counts that row too.
		want := "↑ 1 earlier"
		if lines < blankFrom {
			want = "↑ 2 earlier"
		}
		if got := strings.TrimSpace(textOf(a.frameRows().Rows[2], a.columns)); got != want || strings.Contains(strings.Join(agentBody(a), "\n"), oldest) {
			t.Errorf("%d rows, live: %q, want %q", lines, got, want)
		}
		for _, k := range []string{"up", "home", "pageup"} {
			press(a, "end", k)
			if body := strings.Join(agentBody(a), "\n"); a.scroll != 1 || !strings.Contains(body, oldest) || strings.Contains(body, "earlier") {
				t.Errorf("%d rows, %s: the oldest row %q never shows (scroll %d)\n%s", lines, k, oldest, a.scroll, body)
			}
		}
	}
}

// docs/ux.md §10: the scenes of the AGENT goldens, on the CUJ plan
// (card-highlight pinned, its files plans.go, live.go and plans_test.go):
// a followed plan's line, a prompt that carried the pin, a run of
// thoughts, a shell command, an edit on the pin, one off it (hard), one
// next to it (soft), a failed step and the next command.
func agentScene(t *testing.T, scene string) *app {
	t.Helper()
	a, c, _ := cujApp(t)
	repo := a.plans.Repo
	wire(a, wired("claude"))
	press(a, "\t")
	n := 0
	ev := func(e watch.Event) watch.Event {
		n++
		if e.Meta == nil {
			e.Meta = map[string]any{}
		}
		e.Meta["cwd"] = repo
		return at(c, time.Duration(n)*time.Second, e)
	}
	step := func(id, rel string) []watch.Event {
		p := filepath.Join(repo, rel)
		return []watch.Event{
			ev(watch.Event{Source: "claude", Kind: "command", Text: "Edit " + p, Meta: map[string]any{"tool": "Edit", "path": p, "tool_use_id": id}}),
			ev(watch.Event{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Edit", "path": p, "tool_use_id": id, "ok": true}}),
		}
	}
	bash := func(cmd string) watch.Event {
		return ev(watch.Event{Source: "claude", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "Bash", "command": cmd}})
	}
	prompt := carried(a, "Start on the pinned task: the selected card should be one block. Keep the fill on all three rows.")
	prompt.Meta["cwd"] = repo
	turn := []watch.Event{at(c, 0, prompt)}
	for _, s := range []string{"Looking at the card painter.", "The fill is drawn per row.", "One painter for the bar and the fill, then."} {
		turn = append(turn, ev(watch.Event{Source: "claude", Kind: "thought", Text: s}))
	}
	turn = append(turn, bash("go test ./internal/tui -run PlanCard"))
	turn = append(turn, step("e1", "internal/tui/plans.go")...)
	turn = append(turn, step("e2", "internal/watch/format.go")...)
	turn = append(turn, step("e3", "internal/tui/live_view.go")...)
	if scene == "asking" {
		turn = append(turn, ev(watch.Event{Source: "claude", Kind: "question", Text: "Keep the old card style too, or drop it?"}))
	} else {
		turn = append(turn, bash("go vet ./..."), ev(watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go vet ./...: plans.go:88: unreachable code", Meta: map[string]any{"tool": "Bash", "command": "go vet ./..."}}), bash("go test ./internal/tui"))
	}
	if scene == "settled" || scene == "scrolled" {
		turn = append(turn, ev(watch.Event{Source: "claude", Kind: "text", Text: "## One block\n\nThe selected card is one painter now:\n- the bar and the fill share it\n- the fill starts at column 0\n\n```\ngo test ./internal/tui\n```\n\ngo vet and go test pass."}))
	}
	switch scene {
	case "warm-started":
		// What the agent did before Rex started, then a prompt since.
		a.warmStart(turn[:9], c.t.Add(time.Minute))
		a.ingest([]watch.Event{ev(watch.Event{Source: "claude", Kind: "user", Text: "now the footer"})})
	case "scrolled":
		var earlier []watch.Event
		for i := 0; i < 20; i++ {
			earlier = append(earlier, at(c, -time.Duration(30-i)*time.Second, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("Earlier reply %d.", i), Meta: map[string]any{"cwd": repo}}))
		}
		a.ingest(earlier)
		a.tapeLine("plan", "following Send-off demo")
		a.ingest(turn)
		press(a, "up", "up", "up", "up", "up", "up")
	case "empty":
	default:
		a.tapeLine("plan", "following Send-off demo")
		a.ingest(turn)
	}
	return a
}

// agentScenes are the AGENT goldens (docs/ux.md §10): mid-turn, after the
// turn, asking, scrolled, warm-started, and empty (the feed's health).
var agentScenes = []string{"live", "settled", "asking", "scrolled", "warm-started", "empty"}

// docs/ux.md §10: each AGENT scene's text column at 80×24 and 120×32, in
// NO_COLOR, and at 80×24 in color (its SGR written as \e), against the
// goldens in testdata/agent; -update rewrites them.
func TestAgentGoldens(t *testing.T) {
	for _, scene := range agentScenes {
		var plain, color []string
		for _, size := range [][2]int{{80, 24}, {120, 32}} {
			a := agentScene(t, scene)
			if scene == "empty" {
				a.log = nil // the scene's turn never came
				a.health = feedHealth{path: filepath.Join(os.Getenv("HOME"), ".rex", "session.jsonl"), statusesFor: func(string) []watch.HarnessStatus {
					return []watch.HarnessStatus{
						{Name: "claude", Present: true, Wired: true, Prompts: true},
						{Name: "codex"},
						{Name: "cursor", Present: true, Wired: true, Prompts: true, Partial: true},
						{Name: "opencode"},
					}
				}}
			}
			a.settle(size[0], size[1])
			column := textColumn(a.layout(), a.lines, a.composeOptions())
			plain = append(plain, fmt.Sprintf("%d×%d", size[0], size[1]))
			for _, row := range column {
				plain = append(plain, strings.TrimRight(row, " "))
			}
			if size[0] == 80 {
				a.enabled = true
				for _, row := range textColumn(a.layout(), a.lines, a.composeOptions()) {
					color = append(color, strings.ReplaceAll(strings.TrimRight(row, " "), "\033", `\e`))
				}
			}
		}
		for name, got := range map[string]string{scene + ".txt": strings.Join(plain, "\n") + "\n", scene + ".color.txt": strings.Join(color, "\n") + "\n"} {
			golden := filepath.Join("testdata", "agent", name)
			if *update {
				os.MkdirAll(filepath.Dir(golden), 0o755)
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (go test ./internal/tui -run AgentGoldens -update)", err)
			}
			if got != string(want) {
				t.Errorf("%s:\n%s\nwant\n%s", golden, got, want)
			}
		}
	}
}

// docs/ux.md §10: the scenes keep their promises: the off-pin edit's tag
// is the pin's yellow and the soft one muted, the pin's id under the
// prompt yellow, only the ✗ and a turn's error red, and the color frame's
// text is the NO_COLOR frame's.
func TestAgentScenesRoles(t *testing.T) {
	a := agentScene(t, "settled")
	a.settle(80, 32)
	a.enabled = true
	color := textColumn(a.layout(), a.lines, a.composeOptions())
	a.enabled = false
	plain := textColumn(a.layout(), a.lines, a.composeOptions())
	for i := range plain {
		if stripSGR(color[i]) != plain[i] {
			t.Fatalf("row %d: %q vs %q", i, stripSGR(color[i]), plain[i])
		}
	}
	find := func(s string) string {
		for i, row := range plain {
			if strings.Contains(row, s) {
				return color[i]
			}
		}
		t.Fatalf("no row has %q\n%s", s, strings.Join(plain, "\n"))
		return ""
	}
	yellow, muted, red := fg("accent"), fg("muted"), fg("error")
	if row := find("format.go"); !strings.Contains(row, yellow+"off pin") {
		t.Errorf("hard drift's tag is yellow: %q", row)
	}
	if row := find("live_view.go"); !strings.Contains(row, muted+"off pin?") || strings.Contains(row, yellow) {
		t.Errorf("soft drift's tag is muted: %q", row)
	}
	if row := find("with pin"); !strings.Contains(row, yellow+"card-highlight") {
		t.Errorf("the pin's id under the prompt: %q", row)
	}
	if row := find("unreachable code"); !strings.Contains(row, red+"✗") || strings.Count(row, red) != 1 {
		t.Errorf("only the ✗ is red: %q", row)
	}
	if row := find("go test ./internal/tui -run PlanCard"); !strings.Contains(row, muted+"  $ go test") {
		t.Errorf("a step is muted: %q", row)
	}
}

func fg(role string) string { return mascot.FG(mascot.UI[role]) }
