package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

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

// driftEvent is Rex's drift block as noteDrift writes it: its rows in
// meta, and the same rows as text.
func driftEvent(pin string, rows ...driftNote) watch.Event {
	var text []string
	for _, n := range rows {
		text = append(text, fmt.Sprintf("%s %s · %s", n.d.Level, n.d.Subject, n.d.Reason))
	}
	return watch.NewEvent("rex", "drift", strings.Join(text, "\n"), map[string]any{"pin": pin, "rows": rows})
}

// pathDrift and promptDrift are one row of a drift block.
func pathDrift(level plan.DriftLevel, rel, why string) driftNote {
	return driftNote{d: plan.Drift{Level: level, Subject: rel, Reason: why}, rel: rel}
}

func promptDrift(level plan.DriftLevel, prompt, why string) driftNote {
	return driftNote{d: plan.Drift{Level: level, Subject: prompt, Reason: why}, prompt: prompt}
}

func viewRows(log []watch.Event, width int, opt viewOpt) []string {
	return strings.Split(strings.TrimSuffix(viewText(BuildLogView(log, width, opt)), "\n"), "\n")
}

// docs/ux.md §10: a run of thoughts is one row, "thinking ×N" and the
// latest; live (newest, the turn in flight) it goes on over a second
// row from 12 rows; one thought has no ×1; under 30 cells only the label.
func TestThoughtRunFolds(t *testing.T) {
	run := thoughts("one", "two", "three", "four", "Looking at the plans view first; the card border is drawn apart.")
	settled := viewRows(run, 38, viewOpt{tall: true})
	if len(settled) != 1 || settled[0] != "thinking ×5  Looking at the plans vie…" {
		t.Fatalf("settled: %q", settled)
	}
	// The label is bold (docs/ux.md §5), the latest thought beside it not.
	for _, w := range []int{38, 24} {
		sp := BuildLogView(run, w, viewOpt{tall: true})[0].Spans
		if len(sp) == 0 || sp[0] != (Span{Text: "thinking ×5", Bold: true}) || len(sp) > 1 && sp[1].Bold {
			t.Fatalf("%d cells: the label is bold: %+v", w, sp)
		}
	}
	live := viewRows(run, 38, viewOpt{live: true, tall: true})
	if len(live) != 2 || live[0] != "thinking ×5  Looking at the plans view" || live[1] != "  first; the card border is drawn apa…" {
		t.Fatalf("live: %q", live)
	}
	if short := viewRows(run, 38, viewOpt{live: true}); len(short) != 1 || short[0] != settled[0] {
		t.Fatalf("live under 12 rows is one row: %q", short)
	}
	if one := viewRows(thoughts("Checking the sprite."), 38, viewOpt{tall: true}); one[0] != "thinking  Checking the sprite." {
		t.Fatalf("one thought: %q", one)
	}
	for _, w := range []int{24, 29} {
		if narrow := viewRows(run, w, viewOpt{live: true, tall: true}); len(narrow) != 1 || narrow[0] != "thinking ×5" {
			t.Fatalf("%d cells: %q", w, narrow)
		}
		if one := viewRows(thoughts("hmm"), w, viewOpt{tall: true}); one[0] != "thinking" {
			t.Fatalf("%d cells, one thought: %q", w, one)
		}
	}
	// The latest is shown as prose: Markdown stripped, on one line.
	md := viewRows(thoughts("**Plan:**\n- read `live.go`\n- fix it"), 60, viewOpt{tall: true})
	if md[0] != "thinking  Plan: • read live.go • fix it" {
		t.Fatalf("markdown: %q", md)
	}
	// A short latest thought takes one row even live.
	if fits := viewRows(thoughts("a", "short"), 38, viewOpt{live: true, tall: true}); len(fits) != 1 || fits[0] != "thinking ×2  short" {
		t.Fatalf("fits: %q", fits)
	}
	// Only the newest block is live: a command after the run settles it.
	mixed := viewRows(append(run, commands("go test ./...")...), 38, viewOpt{live: true, tall: true})
	if len(mixed) != 2 || mixed[0] != settled[0] {
		t.Fatalf("a run under a command is settled: %q", mixed)
	}
}

// docs/ux.md §10: one clipped row per command; over 5, all but the last 4
// fold into "$ … +N earlier", so a fold hides at least two and saves a row.
func TestCommandRunCap(t *testing.T) {
	six := commands("Edit a.go", "Edit b.go", "go vet ./...", "Edit c.go", "Edit d.go", "go test ./internal/tui -run PlanCardSelection -count=1 -v")
	got := viewRows(six, 38, viewOpt{tall: true})
	want := []string{"$ … +2 earlier", "$ go vet ./...", "$ Edit c.go", "$ Edit d.go", "$ go test ./internal/tui -run PlanCar…"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if four := viewRows(six[:4], 38, viewOpt{tall: true}); len(four) != 4 || strings.Contains(strings.Join(four, ""), "earlier") {
		t.Fatalf("four fit: %q", four)
	}
	// Five show whole: "+1 earlier" would hide one and save no row.
	if five := viewRows(six[:5], 38, viewOpt{tall: true}); len(five) != 5 || five[0] != "$ Edit a.go" || strings.Contains(strings.Join(five, ""), "earlier") {
		t.Fatalf("five: %q", five)
	}
	for n := 1; n <= 12; n++ {
		run := viewRows(commands(strings.Split(strings.Repeat("x,", n), ",")[:n]...), 38, viewOpt{tall: true})
		if len(run) > 5 || strings.Contains(run[0], "+1 earlier") || n > 5 && run[0] != fmt.Sprintf("$ … +%d earlier", n-4) {
			t.Fatalf("%d commands: %q", n, run)
		}
	}
	fold := BuildLogView(six, 38, viewOpt{})[0]
	if len(fold.Spans) != 1 || fold.Spans[0].Role != "muted" {
		t.Fatalf("the fold line is muted: %+v", fold)
	}
}

// docs/ux.md §10: a failed tool call is one row, "✗ <what> · <why>"; when
// it doesn't fit, the command clips first and the reason keeps at least
// half the row. The turn's own error takes up to 3 rows.
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
		{fail("Bash failed · "+cmd+": panic: runtime error: index out of range [3] with length 3", map[string]any{"tool": "Bash", "command": cmd}), 38, "✗ go test ./int… · panic: runtime err…"},
	} {
		got := viewRows([]watch.Event{tc.ev}, tc.width, viewOpt{tall: true})
		if len(got) != 1 || got[0] != tc.want || cellLen(got[0]) > tc.width {
			t.Errorf("%q at %d:\n got %q\nwant %q", tc.ev.Text, tc.width, got, tc.want)
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
		if short := viewRows([]watch.Event{fail(text, nil)}, 38, viewOpt{}); len(short) != 1 || short[0] != want {
			t.Errorf("%q: %q, want %q", text, short, want)
		}
	}
}

// docs/ux.md §9a, §10: a drift row is laid out like a failed tool call,
// "<level> <subject> · <why>": the subject clips first and the reason
// keeps at least half the row. A path clips from the left, so the end of
// its file name stays; a prompt and a shell command clip from the right.
func TestDriftRowsKeepTheirReason(t *testing.T) {
	deep := "server/a/very/deep/path/to/some/session_handler_impl.go"
	log := []watch.Event{driftEvent("login-form",
		pathDrift(plan.Hard, deep, "outside login-form files"),
		promptDrift(plan.Soft, "before the form, sketch session-store and tell me how cookies work", "names session-store, not the pin"),
		pathDrift(plan.Soft, "internal/tui/live_view.go", "next to live.go"),
	)}
	want := []string{
		"rex › drift · login-form",
		"  hard …_impl.go · outside login-form…",
		"  soft before t… · names session-stor…",
		"  soft live_view.go · next to live.go",
	}
	if got := viewRows(log, 38, viewOpt{tall: true}); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("38 cells:\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// With room, a path keeps as many of its last folders as fit.
	if got := viewRows(log, 78, viewOpt{tall: true})[1]; got != "  hard …/deep/path/to/some/session_handler_impl.go · outside login-form files" {
		t.Fatalf("78 cells: %q", got)
	}
	// A failed write names its file the same way; a command clips from the
	// right, as it reads.
	write := watch.Event{Source: "claude", Kind: "error", Text: "Write failed · internal/tui/very/deep/view.go: permission denied", Meta: map[string]any{"tool": "Write", "path": "/r/internal/tui/very/deep/view.go"}}
	if got := viewRows([]watch.Event{write}, 38, viewOpt{tall: true}); len(got) != 1 || got[0] != "✗ …/deep/view.go · permission denied" {
		t.Fatalf("a failed write: %q", got)
	}
	patch := watch.Event{Source: "codex", Kind: "error", Text: "apply_patch failed · internal/tui/session_handler_impl.go: Failed to find expected lines", Meta: map[string]any{"tool": "apply_patch", "path": "/r/internal/tui/session_handler_impl.go"}}
	if got := viewRows([]watch.Event{patch}, 38, viewOpt{tall: true}); len(got) != 1 || got[0] != "✗ …ndler_impl.go · Failed to find exp…" {
		t.Fatalf("a failed patch: %q", got)
	}
	for _, w := range []int{24, 30, 38, 50} {
		for _, row := range viewRows(append(log, write, patch), w, viewOpt{tall: true}) {
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
// ask and around a prompt; thoughts, commands and both ✗ lines pack.
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
		"reply ────────────────────────────────",
		"  Done.",
		"",
		"                      now fix the test",
		"",
		"thinking  The test still fails.",
		"$ go test ./...",
		"✗ go test ./... · exit status 1",
		"✗ error · rate limited",
		"",
		"                                 go on",
		"",
		"✗ error · turn ended with an error",
	}
	if got := viewRows(log, 38, viewOpt{tall: true}); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// docs/ux.md §10: each reply is its own block under "reply ───", its
// Markdown indented 2: list items hang under their text, headings bold,
// code muted.
func TestReplyBlocks(t *testing.T) {
	log := []watch.Event{
		{Source: "claude", Kind: "text", Text: "The selected card is one block now.\n- plans.go draws the card through one painter\n- live.go starts the fill at column 0"},
		{Source: "claude", Kind: "text", Text: "## Next\n\nRun it:\n\n```\ngo test ./...\n```"},
	}
	view := BuildLogView(log, 38, viewOpt{tall: true})
	want := []string{
		"reply ────────────────────────────────",
		"  The selected card is one block now.",
		"  • plans.go draws the card through",
		"    one painter",
		"  • live.go starts the fill at column",
		"    0",
		"",
		"reply ────────────────────────────────",
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
		case "Run it:":
			if l.Spans[1].Bold {
				t.Errorf("prose is not bold: %+v", l)
			}
		}
	}
	if head := view[0]; !head.Spans[0].Bold || head.Spans[1].Role != "border" {
		t.Fatalf("the reply head: %+v", head.Spans)
	}
	// Short screens drop the blank rows, inside a reply too.
	if short := viewText(BuildLogView(log, 38, viewOpt{})); strings.Contains(short, "\n\n") {
		t.Fatalf("blank rows under 12 rows\n%s", short)
	}
}

// docs/ux.md §10, §9a: Rex's own lines start with "rex ›": a plan reload
// is one row, with no "(file)"; drift is "rex › drift · <pin>" and a row
// per path, hard ones red and soft ones yellow.
func TestRexLines(t *testing.T) {
	log := []watch.Event{
		watch.NewEvent("rex", "plan", "card-highlight → active · plan → active", nil),
		driftEvent("card-highlight", pathDrift(plan.Hard, "README.md", "outside card-highlight files"), pathDrift(plan.Soft, "internal/tui/live_view.go", "next to live.go")),
	}
	view := BuildLogView(log, 38, viewOpt{tall: true})
	want := []string{
		"rex › plan · card-highlight → active…",
		"rex › drift · card-highlight",
		"  hard README.md · outside card-highl…",
		"  soft live_view.go · next to live.go",
	}
	if got := strings.TrimSuffix(viewText(view), "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if !view[0].Spans[0].Bold || view[0].Spans[0].Text != "rex ›" || view[0].Spans[1].Role != "muted" {
		t.Fatalf("rex › is bold, the line muted: %+v", view[0].Spans)
	}
	if view[2].Spans[0].Role != "error" || view[3].Spans[0].Role != "accent" {
		t.Fatalf("hard red, soft yellow: %+v %+v", view[2].Spans, view[3].Spans)
	}
	// From the app: a reload's line and an attach's.
	a, c, path := openDemo(t, demoPlan, 120, 32)
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)), 0o644)
	later(a, c)
	tape := viewText(a.logView(78))
	if !strings.Contains(tape, "rex › plan · ") || strings.Contains(tape, "(file)") {
		t.Fatalf("a reload's line\n%s", tape)
	}
}

// docs/ux.md §10: only a reply and the before-rex-started line have a
// rule; every other block is named by its first characters.
func TestOnlyTwoRules(t *testing.T) {
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
		driftEvent("x", pathDrift(plan.Soft, "a.go", "next to b.go")),
	)
	old := regexp.MustCompile(`^── (thinking|run|plan|drift|ask|reply|error)`)
	rules := 0
	for _, w := range []int{24, 38, 78} {
		for _, l := range BuildLogView(log, w, viewOpt{tall: true}) {
			if old.MatchString(l.Text) {
				t.Errorf("%d: an old rule: %q", w, l.Text)
			}
			if strings.Contains(l.Text, "──") {
				rules++
				if !strings.HasPrefix(l.Text, "reply ─") && !strings.HasPrefix(l.Text, "── before rex started ─") {
					t.Errorf("%d: a rule that isn't the reply's or the warm start's: %q", w, l.Text)
				}
			}
		}
	}
	if rules != 6 {
		t.Fatalf("%d rules, want 2 per width", rules)
	}
	// No blank row right under the warm-start line.
	if v := BuildLogView(log, 78, viewOpt{tall: true}); v[1].Kind == "gap" {
		t.Fatalf("a gap under the warm start: %q", viewText(v))
	}
}

// docs/ux.md §10: an ask is an [ASK] pill and the question, wrapped under
// the question, with a blank row before it only on tall screens.
func TestAskBlock(t *testing.T) {
	log := append(thoughts("a"), watch.Event{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./internal/tui ./internal/watch ./internal/cli -count=1)?"})
	tall := viewRows(log, 38, viewOpt{tall: true})
	at := -1
	for i, row := range tall {
		if strings.HasPrefix(row, "[ASK] Allow") {
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
		if !strings.HasPrefix(row, "      ") || strings.HasPrefix(row, "       ") || cellLen(row) > 38 {
			t.Fatalf("not hung under the question: %q", tall)
		}
	}
	for _, row := range viewRows(log, 38, viewOpt{}) {
		if strings.TrimSpace(row) == "" {
			t.Fatalf("a blank row when short: %q", viewRows(log, 38, viewOpt{}))
		}
	}
	v := BuildLogView(log, 38, viewOpt{tall: true})
	if sp := v[at].Spans; len(sp) < 2 || sp[1].Text != "ASK" || !sp[1].Bold {
		t.Fatalf("no bold ASK pill: %+v", sp)
	}
}

// docs/ux.md §10: a prompt keeps its blank rows only on tall screens.
func TestPromptBlankRows(t *testing.T) {
	log := []watch.Event{{Source: "claude", Kind: "user", Text: "make it one block\n\nbar and fill"}}
	blanks := func(opt viewOpt) int {
		n := 0
		for _, row := range viewRows(log, 38, opt) {
			if strings.TrimSpace(row) == "" {
				n++
			}
		}
		return n
	}
	if n := blanks(viewOpt{}); n != 0 {
		t.Fatalf("short: %d blank rows in %q", n, viewRows(log, 38, viewOpt{}))
	}
	if n := blanks(viewOpt{tall: true}); n != 1 {
		t.Fatalf("tall: %d blank rows in %q", n, viewRows(log, 38, viewOpt{tall: true}))
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
			if size[1] < agentTitleRows && blank != 0 {
				t.Errorf("%s %dx%d: %d blank rows\n%s", name, size[0], size[1], blank, strings.Join(rows, "\n"))
			}
			if size[1] >= agentTitleRows && blank == 0 {
				t.Errorf("%s %dx%d: tall screens space blocks\n%s", name, size[0], size[1], strings.Join(rows, "\n"))
			}
			if name == "prompt" && size[0] >= 80 && !strings.Contains(strings.Join(rows, "\n"), "bar and fill") {
				t.Errorf("%dx%d: the prompt is off screen\n%s", size[0], size[1], strings.Join(rows, "\n"))
			}
		}
	}
}

// turnApp is on AGENT after a whole turn: a prompt, thoughts, commands,
// a failed step, an ask and a reply.
func turnApp(t *testing.T) *app {
	t.Helper()
	a, _, _ := cujApp(t)
	press(a, "\t")
	log := []watch.Event{{Source: "claude", Kind: "user", Text: "make the selected card one block\n\nbar and fill on all three rows"}}
	log = append(log, thoughts("Looking at the plans view.", "The fill is per row.")...)
	log = append(log, commands("Edit internal/tui/plans.go", "Edit internal/tui/live.go", "go vet ./...", "Edit internal/tui/plans_test.go", "go test ./internal/tui")...)
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

// docs/ux.md §10: the view is cached per (revision, width, live, tall), so
// a turn that ends with nothing drawn (Cursor's stop) settles the live run.
func TestLogViewRebuildsOnTurn(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	a.ingest([]watch.Event{{Source: "cursor", Kind: "user", Text: "go", TS: 1}})
	a.ingest(thoughts("one", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell, which the card painter skips today."))
	if a.turn != TurnThinking {
		t.Fatalf("turn %v", a.turn)
	}
	live, rev := a.logLines(), a.logRev
	if v := viewText(a.logView(PaneTextWidth(a.columns))); !strings.Contains(v, "thinking ×2") || live < 2 {
		t.Fatalf("live\n%s", v)
	}
	if !a.ingest([]watch.Event{{Source: "cursor", Kind: "idle", Text: "turn completed", TS: 5}}) {
		t.Fatal("a turn end repaints")
	}
	if a.logRev != rev || a.turn != TurnIdle || a.logLines() != live-1 {
		t.Fatalf("settled with no new line: rev %d→%d, %d→%d lines", rev, a.logRev, live, a.logLines())
	}
	if strings.Contains(a.frame(), "  so the fill") {
		t.Fatalf("the second row is gone\n%s", a.frame())
	}
}

// The cap's trim is measured on the head of the stream alone (docs/ux.md
// §10): it must take as many rows as laying out the whole log twice says.
func TestTrimmedRows(t *testing.T) {
	kinds := []watch.Event{
		thoughts("one")[0],
		thoughts("The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell.")[0],
		// 28 cells: one row after "thinking  " at 38, two after "thinking ×2  ".
		thoughts("abcdefghij abcdefghij abcdef")[0],
		commands("go test ./...")[0],
		{Source: "claude", Kind: "command", Text: "Read internal/tui/live.go", Meta: map[string]any{"tool": "Read", "path": "internal/tui/live.go"}},
		{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": "README.md", "ok": true}},
		{Source: "claude", Kind: "user", Text: "make the selected card one block, with the bar and the fill on all three rows of it"},
		{Source: "claude", Kind: "text", Text: "## Done\n\nThe card is one block.\n\n- one painter\n\n```go\nfill(0)\n```"},
		{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)?"},
		{Source: "claude", Kind: "error", Text: "Bash failed · go vet: exit status 1", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "error", Text: "turn failed: the model hit its context limit"},
		watch.NewEvent("rex", "before", "before rex started", nil),
		watch.NewEvent("rex", "plan", "following sendoff-demo", nil),
		{Source: "claude", Kind: "idle", Text: "turn completed"},
	}
	// One filter with the feed's (watch.KeepInPane): a file look, a result
	// and a turn end draw nothing.
	if !hiddenInStream(kinds[4]) || !hiddenInStream(kinds[5]) || !hiddenInStream(kinds[len(kinds)-1]) {
		t.Fatal("the file look, the result and the turn end should draw nothing")
	}
	seed := uint32(1)
	next := func(n int) int { // a fixed sequence, so a failure replays
		seed = seed*1664525 + 1013904223
		return int(seed>>8) % n
	}
	// A run trimmed from ×10 to ×9 narrows its label, so the newest live
	// run takes one row fewer: the head must reach past the whole run.
	run := thoughts("abcdefghij abcdefghij abc") // 25 cells
	for len(run) < 10 {
		run = append(run, run[0])
	}
	for round := 0; round <= 300; round++ {
		log := run
		if round > 0 {
			log = make([]watch.Event, 1+next(14))
			for i := range log {
				log[i] = kinds[next(len(kinds))]
			}
		}
		for _, width := range []int{20, 38, 80} {
			for _, opt := range []viewOpt{{}, {live: true}, {tall: true}, {live: true, tall: true}} {
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
	top := 2
	if a.lines >= agentTitleRows {
		top = 3
	}
	var out []string
	for _, r := range rows[top : len(rows)-1] {
		out = append(out, textOf(r, a.columns))
	}
	return out
}

// liveApp is on AGENT with a reply every 3 rows above a prompt and a live
// 2-row thought run, so one row of movement shows.
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
// keeps its first visible line while a run grows, settles (the turn's end,
// or a drawn block under it) or the log trims; End is live again.
func TestScrolledViewStable(t *testing.T) {
	a := liveApp(t)
	livePlain := a.logLines()
	press(a, "up", "up", "up")
	if a.scroll != 3 || a.logLines() != livePlain || !a.viewOpt().live {
		t.Fatalf("scrolled up, the run stays live: scroll %d, %d→%d lines", a.scroll, livePlain, a.logLines())
	}
	top := func() string { return strings.Join(agentBody(a), "\n") }
	first := top()
	steps := []struct {
		what string
		evs  []watch.Event
		news int
	}{
		{"a thought joins the run", thoughts("and another"), 0},
		{"the turn ends", []watch.Event{{Source: "claude", Kind: "idle", Text: "turn completed", TS: 50}}, 0},
		{"a command", commands("go test ./..."), 1},
	}
	for _, s := range steps {
		before := a.unseenLines
		a.ingest(s.evs)
		if got := top(); got != first {
			t.Fatalf("%s moved the view: %q → %q", s.what, first, got)
		}
		if a.unseenLines-before != s.news {
			t.Fatalf("%s: %d new, want %d", s.what, a.unseenLines-before, s.news)
		}
	}
	// The live run just under the view, or further down, settles without
	// moving it: by the turn's end, which draws nothing, or by a command,
	// which draws a block under it.
	for _, up := range []int{1, 2, 3} {
		for _, end := range []struct {
			what string
			evs  []watch.Event
		}{
			{"the turn ends", []watch.Event{{Source: "claude", Kind: "idle", Text: "turn completed", TS: 50}}},
			{"a command", commands("go vet ./...")},
		} {
			a := liveApp(t)
			for i := 0; i < up; i++ {
				press(a, "up")
			}
			body := agentBody(a)
			a.ingest(end.evs)
			if a.viewOpt().live && end.what == "the turn ends" || a.scroll < 1 {
				t.Fatalf("%d↑, %s: live %v, scroll %d", up, end.what, a.viewOpt().live, a.scroll)
			}
			// At 1↑ the run's first row is the view's last, and it settles
			// in place; when the turn's end gives that row back, the view
			// stays scrolled (never under 1) and so moves the one row.
			got := agentBody(a)
			if up == 1 {
				if end.what == "the turn ends" {
					continue
				}
				body, got = body[:len(body)-1], got[:len(got)-1]
			}
			if strings.Join(got, "\n") != strings.Join(body, "\n") {
				t.Fatalf("%d↑, %s moved the view:\n%s\n→\n%s", up, end.what, strings.Join(body, "\n"), strings.Join(got, "\n"))
			}
		}
	}
	// The cap drops the oldest events off the top: the view stays.
	var more []watch.Event
	for i := 0; i < MaxLog; i++ {
		more = append(more, watch.Event{Source: "claude", Kind: "text", Text: fmt.Sprintf("later %d", i), TS: float64(100 + i)})
	}
	a.ingest(more[:MaxLog-len(a.log)])
	press(a, "end")
	for i := 0; i < 40; i++ {
		press(a, "up")
	}
	first = top()
	a.ingest(more[MaxLog-30:])
	if got := top(); got != first || a.scroll == 0 {
		t.Fatalf("a trim moved the view: %q → %q (scroll %d)", first, got, a.scroll)
	}
	press(a, "end")
	if a.scroll != 0 || a.unseenLines != 0 {
		t.Fatalf("end is live: scroll %d, %d new", a.scroll, a.unseenLines)
	}
}

// docs/ux.md §10: a run is live while the turn is in flight, so a tool call
// that draws nothing (a file look between thoughts) leaves the stream
// still: it doesn't settle the run only for the next thought to grow it.
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
	// The next drawn block settles it.
	live := a.logLines()
	a.ingest(commands("go vet ./..."))
	if a.logLines() != live {
		t.Fatalf("a command under the run: %d→%d rows, want the run's row given back", live, a.logLines())
	}
}

// docs/ux.md §10: scrolling never changes the layout, so one ↑ from the
// bottom moves the view one row, and ↓ moves it back.
func TestUpFromLiveMovesOneRow(t *testing.T) {
	a := liveApp(t)
	if a.turn != TurnThinking {
		t.Fatalf("turn %v", a.turn)
	}
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

// docs/ux.md §10: when the live run's second row is all that overflows,
// ↑, Home and PgUp still reach the oldest row.
func TestOldestRowReachableWhileLive(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	bodyH := PaneBodyHeight(a.lines)
	var log []watch.Event
	for i := 0; i < bodyH-1; i++ {
		cmd := fmt.Sprintf("x%02d", i)
		log = append(log,
			watch.Event{Source: "claude", Kind: "command", Text: cmd, Meta: map[string]any{"tool": "Bash"}, TS: float64(i)},
			watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · " + cmd + ": boom", Meta: map[string]any{"tool": "Bash", "command": cmd}, TS: float64(i)})
	}
	a.ingest(log)
	a.ingest(thoughts("The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell."))
	for a.logLines() > bodyH+1 {
		a.log = a.log[1:]
		a.logRev++
	}
	a.settle(a.columns, a.lines)
	if a.logLines() != bodyH+1 || !a.viewOpt().live {
		t.Fatalf("setup: %d rows, body %d", a.logLines(), bodyH)
	}
	oldest := strings.SplitN(viewText(a.logView(PaneTextWidth(a.columns))), "\n", 2)[0]
	for _, k := range []string{"up", "home", "pageup"} {
		press(a, "end", k)
		if body := strings.Join(agentBody(a), "\n"); a.scroll != 1 || !strings.Contains(body, oldest) {
			t.Errorf("%s: the oldest row %q never shows (scroll %d)\n%s", k, oldest, a.scroll, body)
		}
	}
}

// docs/ux.md §10, §12: AGENT at 80×12, 24×12, 80×24 and 80×7 (its
// mockups and the layout tiers), NO_COLOR, footers as shipped.
func TestAgentMockups(t *testing.T) {
	frame := func(a *app, cols, lines int) string {
		a.settle(cols, lines)
		var out []string
		for _, row := range a.frameRows().Rows {
			out = append(out, textOf(row, cols))
		}
		return strings.Join(out, "\n")
	}
	turn := func(t *testing.T, last ...watch.Event) *app {
		a, c, _ := cujApp(t)
		press(a, "\t")
		ev := func(d int, e watch.Event) watch.Event { return at(c, time.Duration(d)*time.Second, e) }
		a.ingest([]watch.Event{ev(1, watch.Event{Source: "claude", Kind: "user", Text: "make the selected card one block"})})
		a.tapeLine("plan", "card-highlight → active")
		var evs []watch.Event
		run := append(thoughts("a", "b", "c", "Looking at the plans view first; the card border is drawn apart."),
			commands("Edit internal/tui/plans.go", "Edit internal/tui/live.go", "go test ./internal/tui")...)
		for i, e := range append(run, last...) {
			evs = append(evs, ev(2+i, e))
		}
		a.ingest(evs)
		return a
	}
	live := turn(t, thoughts("x", "y", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter.")...)
	if got, want := frame(live, 80, 12), strings.Join([]string{
		"PLAN [AGENT] thinking  following …demo",
		"▸ card-highlight  Selected plan…   1/3",
		"  AGENT                          live",
		"",
		"rex › plan · card-highlight → active",
		"thinking ×4  Looking at the plans vie…",
		"$ Edit internal/tui/plans.go",
		"$ Edit internal/tui/live.go",
		"$ go test ./internal/tui",
		"thinking ×3  The bar is drawn per row,",
		"  so the fill has to start at column…",
		"tab plan  ? help  ↑↓ scroll",
	}, "\n"); got != want {
		t.Errorf("38×12, live:\n%s\nwant\n%s", got, want)
	}
	if got, want := frame(live, 24, 12), strings.Join([]string{
		"PLAN [AGENT] thinking",
		"▸ card-highlight     1/3",
		"  AGENT            live",
		"                block",
		"",
		"rex › plan · card-highl…",
		"thinking ×4",
		"$ Edit internal/tui/pla…",
		"$ Edit internal/tui/liv…",
		"$ go test ./internal/tui",
		"thinking ×3",
		"tab plan  ? help",
	}, "\n"); got != want {
		t.Errorf("24×12:\n%s\nwant\n%s", got, want)
	}
	// The §10 mockup at 80×24: the turn over, an edit next to the pin's
	// files on row 1.
	over, c, _ := cujApp(t)
	press(over, "\t")
	over.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "make the selected card one block"})})
	over.tapeLine("plan", "card-highlight → active")
	var evs []watch.Event
	evs = append(evs, thoughts("a", "b", "c", "Looking at the plans view first; the card border is drawn apart.")...)
	evs = append(evs, commands("Edit internal/tui/plans.go", "Edit internal/tui/live.go", "go vet ./...", "Edit internal/tui/plans_test.go", "Edit internal/tui/outline.go", "go test ./internal/tui -run PlanCardSelection")...)
	evs = append(evs, watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go test ./internal/tui -run PlanCardSelection: exit status 1", Meta: map[string]any{"tool": "Bash", "command": "go test ./internal/tui -run PlanCardSelection"}})
	evs = append(evs, thoughts("Two painters.", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter.")...)
	for i := range evs {
		evs[i] = at(c, time.Duration(2+i)*time.Second, evs[i])
	}
	over.ingest(append(evs, edit(over, c, 30*time.Second, "internal/tui/live_view.go")))
	over.ingest([]watch.Event{at(c, 31*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "The selected card is one block now.\n- plans.go draws the card through one painter\n- live.go starts the fill at column 0"})})
	if got, want := frame(over, 80, 24), strings.Join([]string{
		"PLAN [AGENT] idle  following …off-demo",
		"drift · soft: live_view.go         1/3",
		"  AGENT                          live",
		"                     card one block",
		"",
		"rex › plan · card-highlight → active",
		"thinking ×4  Looking at the plans vie…",
		"$ … +2 earlier",
		"$ go vet ./...",
		"$ Edit internal/tui/plans_test.go",
		"$ Edit internal/tui/outline.go",
		"$ go test ./internal/tui -run PlanCar…",
		"✗ go test ./internal/… · exit status 1",
		"thinking ×2  The bar is drawn per row…",
		"rex › drift · card-highlight",
		"  soft live_view.go · next to plans.go",
		"",
		"reply ────────────────────────────────",
		"  The selected card is one block now.",
		"  • plans.go draws the card through",
		"    one painter",
		"  • live.go starts the fill at column",
		"    0",
		"tab plan  ? help  ↑↓ scroll",
	}, "\n"); got != want {
		t.Errorf("38×24, the turn over:\n%s\nwant\n%s", got, want)
	}
	ask := turn(t, append(thoughts("Two of them.", "Two painters draw the card: one for the bar, one for the fill."), watch.Event{Source: "claude", Kind: "question", Text: "Keep the old card style too?"})...)
	if got, want := frame(ask, 80, 7), strings.Join([]string{
		"PLAN [AGENT] ask  following …doff-demo",
		"▸ card-highlight  Selected plan…   1/3",
		"$ Edit internal/tui/live.go",
		"$ go test ./internal/tui",
		"thinking ×2  Two painters draw the ca…",
		"[ASK] Keep the old card style too?",
		"tab plan  ? help  ↑↓ scroll",
	}, "\n"); got != want {
		t.Errorf("38×7:\n%s\nwant\n%s", got, want)
	}
}
