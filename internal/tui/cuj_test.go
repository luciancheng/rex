package tui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// cujFixture is the CUJ plan, a copy of rex/plans/cuj/sendoff-demo.rex.json,
// so trying Rex on the repo's own demo can't change what the tests see.
func cujFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "sendoff-demo.rex.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// mustReplace is s with old replaced once. A fixture edit that doesn't
// apply fails the test, so a fixture that changed (or a CRLF checkout of
// it) never runs a test on the wrong bytes.
func mustReplace(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("fixture edit didn't apply: %q", old)
	}
	return strings.Replace(s, old, new, 1)
}

// cujApp is Rex started in a repo that holds the CUJ plan, opened from
// Plans' Open latest card, so it is followed and on its task list
// (docs/ux.md §11).
func cujApp(t *testing.T) (*app, *clock, string) {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(PlansDir(repo), "cuj", "sendoff-demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, cujFixture(t), 0o644)
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	if press(a, "\r"); a.at != atOutline || !a.follow {
		t.Fatalf("setup: Open latest opens the CUJ plan: at=%d", a.at)
	}
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	return a, c, path
}

// keys counts keystrokes so the loop can be held to the budget in
// docs/ux.md §14.
type keys struct {
	a *app
	n int
}

func (k *keys) press(ks ...string) { k.n += len(ks); press(k.a, ks...) }
func (k *keys) typ(s string)       { k.n += len([]rune(s)); typeText(k.a, s) }
func (k *keys) spent(t *testing.T, budget int, what string) {
	t.Helper()
	if k.n > budget {
		t.Errorf("%s took %d keys, budget %d", what, k.n, budget)
	}
	k.n = 0
}

// The journey (docs/ux.md §14): Rex starts on Plans → the agent writes a
// plan and Rex opens it → the agent pins a task → an off-pin edit shows
// drift → the agent marks the task done → Tab to the tape and back → Esc
// to Plans, where Continue follows and any other pick latches → the agent
// writes another plan, which o opens → quit → start again on Continue,
// one enter from last time's plan. The agent drives the plan; Rex only
// shows it and never writes it. At 109×24, a column short of the mascot,
// the same journey is text-only: every frame gives the text the whole
// width up to the measure (docs/ux.md §12).
func TestCUJWholeLoop(t *testing.T) {
	for _, tc := range []struct {
		cols, lines int
		withMascot  bool
	}{{120, 32, true}, {109, 24, false}} {
		t.Run(fmt.Sprintf("%dx%d", tc.cols, tc.lines), func(t *testing.T) { cujWholeLoop(t, tc.cols, tc.lines, tc.withMascot) })
	}
}

func cujWholeLoop(t *testing.T, cols, lines int, withMascot bool) {
	a := testApp(t)
	a.settle(cols, lines)
	// look holds every frame the journey checks to its size's tier.
	look := func(f Frame) Frame {
		t.Helper()
		if got := a.layout().mascotCols > 0; got != withMascot {
			t.Fatalf("%dx%d: the mascot shows: %v", cols, lines, got)
		}
		for i, row := range f.Rows {
			plain := stripSGR(row)
			if !withMascot && (strings.ContainsAny(plain, "▀▄█") || cellLen(plain) != min(cols, maxText)) {
				t.Fatalf("%dx%d: row %d isn't text-only: %q", cols, lines, i, plain)
			}
		}
		return f
	}
	rows := func() Frame { return look(a.frameRows()) }
	frame := func() string { rows(); return a.frame() }
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	k := &keys{a: a}
	repo := a.plans.Repo
	path := filepath.Join(a.plans.Root, "cuj", "sendoff-demo.rex.json")
	agent := func(body []byte) { // the agent edits the plan with its file tool
		os.WriteFile(path, body, 0o644)
		a.ingest([]watch.Event{at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + path, Meta: map[string]any{"tool": "Write", "path": path, "ok": true, "cwd": repo}})})
		later(a, c)
	}

	if a.pane != PanePlans || a.at != atLibrary || !strings.Contains(frame(), "❯ Create a plan ") || !strings.Contains(frame(), "Or ask your agent to plan the work.") || strings.Contains(frame(), "╭") {
		t.Fatalf("A. with no plan yet, start lands on Plans' first run, with no field open\n%s", frame())
	}
	if !strings.Contains(frame(), "Agents learn the plan format from the rex-plan skill:") || rowWith(rows(), "rex ingest --install-skill") != "  rex ingest --install-skill" {
		t.Fatalf("A. with the skill nowhere, the first run says how to teach the agent\n%s", frame())
	}
	before := a.frame()
	for i := 0; i < 10; i++ {
		later(a, c)
	}
	if a.at != atLibrary || a.frame() != before {
		t.Fatalf("A. nothing changes Plans by itself\n%s", frame())
	}

	// B. The agent writes the plan: a stub first, then its tasks, none
	// active yet.
	os.MkdirAll(filepath.Dir(path), 0o755)
	agent(plan.Stub("sendoff-demo"))
	k.spent(t, 0, "create (the agent writes the plan)")
	if a.at != atOutline || a.plans.Current != path || !a.follow || !strings.Contains(frame(), "No tasks yet.") || !strings.Contains(rows().Rows[0], "sendoff-demo ") || !strings.Contains(rows().Rows[0], " · following") {
		t.Fatalf("B. the agent's write opens the plan, followed\n%s", frame())
	}
	if got := a.notice().Text; got != "Your agent wrote cuj/sendoff-demo · opened rex/plans/cuj/sendoff-demo.rex.json" || strings.Contains(frame(), "❯") {
		t.Fatalf("B. the notice says which file opened, and nothing is selectable yet: %q\n%s", got, frame())
	}
	active := cujFixture(t)
	planned := []byte(mustReplace(t, mustReplace(t, string(active), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`), `"status": "active",
  "scope"`, `"status": "pending",
  "scope"`))
	agent(planned)
	k.spent(t, 0, "see the plan (Rex follows the write)")
	if a.outline.sel != "card-highlight" || !strings.Contains(frame(), "❯   card-highlight ") || !strings.Contains(frame(), "plan updated · +3 tasks") {
		t.Fatalf("B. the filled plan lands on its pin: sel=%q\n%s", a.outline.sel, frame())
	}
	if row := rowWith(rows(), "❯   card-highlight"); !strings.HasSuffix(row, " next up") {
		t.Fatalf("B. the task Rex would pin is tagged next up: %q", row)
	}
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "nothing pinned · next up card-highlight · Selected plan card is one block" {
		t.Fatalf("B. row 1 names the next up task, with nothing to type: %q", got)
	}

	// C. The agent starts the task: it writes active, and Rex shows it.
	agent(active)
	k.spent(t, 0, "see the pin")
	if got := a.statusLine().Text; got != "plan updated · card-highlight → active · plan → active" {
		t.Fatalf("C. the agent's pin arrives as a reload: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "card-highlight → active · plan → active" {
		t.Fatalf("C. one plan line on the tape: %+v", last)
	}
	c.t = c.t.Add(toastFor)
	if got := a.statusLine().Text; got != "pinned  card-highlight · Selected plan card is one block" || !strings.HasSuffix(rowWith(rows(), "❯   card-highlight"), " pinned") {
		t.Fatalf("C. row 1 and the list follow the file: %q\n%s", got, frame())
	}
	// One task list: no card, no goal, no TASKS rule (docs/ux.md §6).
	for _, never := range []string{"╭", "[PINNED]", "TASKS", "A teammate opens this plan"} {
		if strings.Contains(frame(), never) {
			t.Fatalf("C. the task list has no %q\n%s", never, frame())
		}
	}
	// What the agent gets is one enter away: the pin's details, the
	// fields of the note (docs/ux.md §6, §7a).
	k.press("\r")
	k.spent(t, 1, "see the pinned task's requirements (enter)")
	if a.at != atTaskDetail || !strings.Contains(rows().Rows[0], "Send-off demo › card-highlight") || !strings.Contains(rows().Rows[1], "task 2 of 3") {
		t.Fatalf("C. enter opens the pin's details on a page of their own\n%s", frame())
	}
	for _, want := range []string{"Your agent gets these fields with every prompt.", "  Does          Bar, border and fill cover all three rows of the selected", "  Needs         the pick and pickbg colors exist in mascot.UI", "  Files         internal/tui/plans.go", "  Builds on     plans-ui (done): Home cards, folder browser"} {
		if !strings.Contains(frame(), want) {
			t.Fatalf("C. the pin's details show %q\n%s", want, frame())
		}
	}
	k.press("esc")
	if a.at != atOutline || a.outline.sel != "card-highlight" {
		t.Fatalf("C. esc goes back to the list, on the same task: at=%d sel=%q", a.at, a.outline.sel)
	}

	// The agent works the task; the old verbs do nothing on the task list.
	k.press("p", "d", "s", "b", PastePrefix+"pin load-outline")
	if got, _ := os.ReadFile(path); string(got) != string(active) || a.draft != "" || !strings.HasPrefix(a.statusLine().Text, "pinned  card-highlight") {
		t.Fatal("C. letters and a paste on the task list change nothing")
	}
	k.n = 0
	write := filepath.Join(repo, "internal/tui/plans.go")
	a.ingest([]watch.Event{at(c, time.Second, watch.Event{Source: "claude", Kind: "user", Text: "work the plan: continue rex/plans/cuj/sendoff-demo.rex.json"})})
	if a.turn != TurnThinking {
		t.Fatalf("D. a prompt starts the turn: %v", a.turn)
	}
	a.ingest([]watch.Event{
		at(c, 2*time.Second, watch.Event{Source: "claude", Kind: "thought", Text: "The card rows are painted separately."}),
		at(c, 3*time.Second, watch.Event{Source: "claude", Kind: "command", Text: "Edit " + write, Meta: map[string]any{"tool": "Edit", "path": write}}),
		at(c, 4*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + write, Meta: map[string]any{"tool": "Edit", "path": write, "ok": true}}),
	})
	if a.turn != TurnRunning || strings.HasPrefix(a.statusLine().Text, "off pin") {
		t.Fatalf("D. a listed file runs the turn and adds no drift: %v %q", a.turn, a.statusLine().Text)
	}
	// D. An edit off the pin shows drift.
	a.ingest([]watch.Event{at(c, 5*time.Second, watch.Event{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "README.md"), "ok": true}})})
	if !strings.HasPrefix(a.statusLine().Text, "off pin · README.md") {
		t.Fatalf("D. drift shows: %q", a.statusLine().Text)
	}

	// E. The agent marks the task done, with its retrospective.
	done := []byte(mustReplace(t, string(active), `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],
      "accomplished": ["the selection bar, border and fill cover all three card rows"],
      "modified": ["internal/tui/plans.go"],
      "verified_how": ["go test ./internal/tui -run PlanCardSelection (passed)"],
      "completed_notes": "the pick colors were already in mascot.UI",`))
	agent(done)
	a.ingest([]watch.Event{at(c, 6*time.Second, watch.Event{Source: "claude", Kind: "text", Text: "Done: one block now. card-highlight is marked done in the plan."})})
	k.spent(t, 0, "mark done (the agent does)")
	if got := a.statusLine().Text; got != "✔ card-highlight done · next up load-outline" {
		t.Fatalf("E. the file finished the pin: %q", got)
	}
	// The list marks it done and tags what is next up; the cursor stays
	// where it was (docs/ux.md §1, §8).
	if a.turn != TurnIdle || !strings.HasPrefix(rowWith(rows(), "❯ ✔ card-highlight"), "❯ ✔ card-highlight") || !strings.HasSuffix(rowWith(rows(), "    load-outline "), " next up") {
		t.Fatalf("E. the reply ends the turn; the list moves on\n%s", frame())
	}
	k.press("\r") // the selection stayed on the task the agent finished
	if a.at != atTaskDetail || a.outline.sel != "card-highlight" {
		t.Fatalf("E. enter on the task opens its details: at=%d sel=%q", a.at, a.outline.sel)
	}
	if head := rows().Rows[0]; !strings.Contains(head, "card-highlight") || !strings.Contains(head, "✔ done") {
		t.Fatalf("E. the details' title names the task and its state: %q", head)
	}
	detail := viewText(taskPage(a.doc, a.plans.CurrentRel(), a.outline.sel, 78)) // it scrolls; all of it
	for _, want := range []string{"  ✔ Done · what your agent wrote when it finished it", "  Did           the selection bar, border and fill cover all three", "  Changed       internal/tui/plans.go", "  Checked       go test ./internal/tui -run PlanCardSelection (passed)", "  Notes         the pick colors were already in mascot.UI", "  Planned       Bar, border and fill"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("E. the details show %q\n%s", want, detail)
		}
	}
	k.press("esc", "i") // About: the goal and the progress, read once a plan
	if about := viewText(aboutPage(a.doc, 78)); a.at != atPlanDetail || !strings.Contains(about, "  Goal          A teammate opens this plan") || !strings.Contains(about, "  Progress      2/3 done · 1 to go") {
		t.Fatalf("E. About has the goal and the progress\n%s", about)
	}
	k.press("esc")
	if a.at != atOutline || a.outline.sel != "card-highlight" {
		t.Fatalf("E. esc from About is the list, on the same task: at=%d sel=%q", a.at, a.outline.sel)
	}
	k.n = 0

	// F. Tab to AGENT and back.
	k.press("\t")
	k.spent(t, 1, "watch the agent")
	// AGENT names each block by its first characters (docs/ux.md §10).
	tape := viewText(BuildLogView(a.log, 78, a.viewOpt()))
	for _, want := range []string{
		"thinking  The card rows are painted separately.",
		"$ Edit internal/tui/plans.go",
		"reply ───",
		"  Done: one block now.",
		"rex › plan · card-highlight → active",
		"rex › plan · card-highlight → done",
		"rex › drift · card-highlight\n  hard README.md · outside card-highlight files",
	} {
		if !strings.Contains(tape, want) || !strings.Contains(frame(), strings.SplitN(want, "\n", 2)[0]) {
			t.Errorf("F. AGENT missing %q\n%s", want, frame())
		}
	}
	if strings.Contains(tape, "wrote ") || strings.Contains(tape, "── ") || strings.Contains(tape, "note") {
		t.Errorf("F. no wrote/done spam, no notes, and no rule but the reply's\n%s", tape)
	}
	logged := len(a.log)
	k.press("p", "d", "s", "b", "x", "\r")
	if len(a.log) != logged || a.draft != "" {
		t.Fatal("F. typing on the tape adds nothing")
	}
	k.n = 0
	k.press("\t")
	k.spent(t, 1, "back to the plan")
	if a.pane != PanePlans || a.at != atOutline {
		t.Fatal("F. Tab returns to the task list")
	}

	// G. Plans: Continue is the plan you left, and enter on it is a round
	// trip that keeps following; create a plan there, open one by name,
	// Open latest and a Recent plan. Any pick but Continue and Open latest
	// latches.
	k.press("esc")
	if a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue || strings.Contains(frame(), "╭") {
		t.Fatalf("G. Esc climbs to Plans, on Continue, with no field open\n%s", frame())
	}
	if row := rowWith(rows(), "❯ Continue"); !strings.HasPrefix(row, "❯ Continue        Send-off demo · 2/3 done") {
		t.Fatalf("G. Continue names the plan you left: %q\n%s", row, frame())
	}
	k.press("\r")
	k.spent(t, 2, "back to the plan (esc enter)")
	if !a.follow || a.at != atOutline {
		t.Fatal("G. enter on Continue keeps following")
	}
	k.press("esc", "down", "\r")
	if !a.plans.Prompting() || !strings.Contains(frame(), "╭ name ─") {
		t.Fatalf("G. Create a plan is New plan, its field open\n%s", frame())
	}
	k.typ("scratch")
	k.press("\r")
	k.spent(t, 3+len("scratch")+1, "create in Rex (esc ↓ enter, name, enter)")
	// scratch is newer than the demo, the agent's last save, by a clock
	// tick on any filesystem; the walk sees it at once.
	stampAfter(t, filepath.Join(a.plans.Root, "scratch.rex.json"), path)
	a.plans.walk = planWalk{}
	stub, err := plan.Load(filepath.Join(a.plans.Root, "scratch.rex.json"))
	if err != nil || len(stub.Problems) != 0 || a.at != atOutline || a.follow || strings.Contains(rows().Rows[0], "following") {
		t.Fatalf("G. create writes a v1 stub, opens it and latches: %v\n%s", err, frame())
	}
	if got := a.notice().Text; got != "✔ Created rex/plans/scratch.rex.json" {
		t.Fatalf("G. the notice says what was written: %q", got)
	}
	k.press("esc")
	k.typ("sendoff-demo")
	if !strings.Contains(frame(), "╭ find ─") {
		t.Fatalf("G. typing on Plans opens the find field\n%s", frame())
	}
	k.press("\r")
	k.spent(t, 1+len("sendoff-demo")+1, "open by name (esc, name, enter)")
	if a.plans.Current != path || a.at != atOutline || a.follow || a.outline.sel != "load-outline" {
		t.Fatalf("G. the plan opens on its pin, latched: sel=%q\n%s", a.outline.sel, frame())
	}
	if got := a.notice().Text; got != "Opened Send-off demo · rex/plans/cuj/sendoff-demo.rex.json" {
		t.Fatalf("G. the notice says which file opened: %q", got)
	}
	// Open latest is the newest plan, scratch, and follows it; then
	// sendoff-demo, which you had open, leads Recent.
	k.press("esc", "down", "down", "down", "\r")
	k.spent(t, 5, "open the newest plan (esc ↓↓↓ enter, Open latest)")
	if filepath.Base(a.plans.Current) != "scratch.rex.json" || a.at != atOutline || !a.follow {
		t.Fatalf("G. Open latest opens scratch, following: %q follow=%v", a.plans.Current, a.follow)
	}
	k.press("esc", "down", "down", "down", "\r")
	k.spent(t, 5, "open a recent plan (esc ↓↓↓ enter, the first under Recent)")
	if a.plans.Current != path || a.at != atOutline || a.follow {
		t.Fatalf("G. the first under Recent opens sendoff-demo, latched: %q follow=%v", a.plans.Current, a.follow)
	}
	// Latched, the agent writes another plan: the notice offers it, enter
	// keeps its meaning, and o opens it, following again (docs/ux.md §4).
	c.t = c.t.Add(toastFor) // the open's notice has had its time
	next := filepath.Join(a.plans.Root, "cuj", "next.rex.json")
	os.WriteFile(next, plan.Stub("next"), 0o644)
	stampAfter(t, next, filepath.Join(a.plans.Root, "scratch.rex.json")) // the newest plan
	a.ingest([]watch.Event{at(c, 0, watch.Event{Source: "claude", Kind: "result", Text: "wrote " + next, Meta: map[string]any{"tool": "Write", "path": next, "ok": true, "cwd": repo}})})
	if got := a.notice().Text; got != "Your agent wrote cuj/next · o opens it" || a.plans.Current != path {
		t.Fatalf("G. a latched plan stays, and the notice offers the new one: %q current=%q", got, a.plans.Current)
	}
	if press(a, "\r"); a.at != atTaskDetail || a.plans.Current != path {
		t.Fatalf("G. enter never takes the offer: at=%d current=%q", a.at, a.plans.Current)
	}
	press(a, "esc")
	k.press("o")
	k.spent(t, 1, "open the plan your agent wrote while latched (o)")
	if a.plans.Current != next || !a.follow || a.at != atOutline {
		t.Fatalf("G. o opens it, following: %q follow=%v", a.plans.Current, a.follow)
	}
	k.press("esc", "down", "down", "down", "\r")
	k.spent(t, 5, "back to a recent plan (esc ↓↓↓ enter)")
	if a.plans.Current != path || a.follow {
		t.Fatalf("G. the first under Recent opens the demo again, latched: %q follow=%v", a.plans.Current, a.follow)
	}

	// H. Quit.
	k.press(CtrlC)
	if !strings.Contains(frame(), "ctrl+c again to quit") {
		t.Fatal("H. the first Ctrl+C asks again")
	}
	k.press(CtrlC)
	k.spent(t, 2, "quit")
	if !a.quit || a.exitLine() != "rex: rex/plans/cuj/sendoff-demo.rex.json · next up load-outline" {
		t.Fatalf("H. quit with the plan named: %q", a.exitLine())
	}
	if got, _ := os.ReadFile(path); string(got) != string(done) {
		t.Fatalf("Rex never wrote the plan: the file is what the agent wrote\n%s", got)
	}

	// I. The next start lands on Plans, Continue last time's plan: one
	// enter opens it, following, on its pin (docs/ux.md §14).
	b := newApp("yellow", false, "idle", NewPlanBrowser(repo), cols, lines)
	if row := b.plans.Rows[b.plans.Cursor]; b.pane != PanePlans || b.at != atLibrary || b.plans.Current != "" || row.Kind != PlanRowContinue || row.Path != path {
		t.Fatalf("I. the next start is Plans, on Continue: at=%d on %+v", b.at, row)
	}
	kb := &keys{a: b}
	kb.press("\r")
	kb.spent(t, 1, "open last time's plan (enter on Continue)")
	if b.at != atOutline || b.plans.Current != path || !b.follow || b.outline.sel != "load-outline" {
		t.Fatalf("I. enter opens it again, following: at=%d current=%q sel=%q", b.at, b.plans.Current, b.outline.sel)
	}
}

// rowWith is the first of f's rows that has s, as its text column (no
// mascot, trailing blanks off); "" for none.
func rowWith(f Frame, s string) string {
	for _, r := range f.Rows {
		if text := textOf(r, cellLen(stripSGR(r))); strings.Contains(text, s) {
			return text
		}
	}
	return ""
}

// matrixScreen is one screen of the frame matrix: how to reach it from a
// fresh start, and how to tell it was reached.
type matrixScreen struct {
	name    string
	empty   bool // starts in a repo with no plans; the rest on the CUJ plan
	setup   func(a *app)
	reached func(a *app) bool
}

// matrixScreens is every screen and overlay Rex draws (AGENTS.md, Commands
// and tests): a new one joins the list.
func matrixScreens(t *testing.T) []matrixScreen {
	list := func(a *app) { press(a, "esc") } // the CUJ plan's task list, Esc to Plans
	lib := func(sc PlanScreen) func(a *app) bool {
		return func(a *app) bool { return a.pane == PanePlans && a.at == atLibrary && a.plans.Screen == sc }
	}
	at := func(p planAt) func(a *app) bool {
		return func(a *app) bool { return a.at == p && a.overlay[a.pane] == overlayNone }
	}
	over := func(o overlayKind) func(a *app) bool { return func(a *app) bool { return a.overlay[a.pane] == o } }
	browse := func(a *app) {
		list(a)
		a.plans.pick(PlanRowOpen)
		press(a, "\r")
	}
	pick := func(a *app, kind PlanRowKind) {
		for i, row := range a.plans.Rows {
			if row.Kind == kind {
				a.plans.Cursor = i
			}
		}
	}
	// rewrite is the agent saving the open plan as src.
	rewrite := func(src string) func(a *app) {
		return func(a *app) {
			os.WriteFile(a.plans.Current, []byte(src), 0o644)
			a.reloadDoc()
		}
	}
	pinned := func(status string) func(a *app) bool {
		return func(a *app) bool {
			if !at(atOutline)(a) || a.doc.plan == nil {
				return false
			}
			pin := a.doc.plan.Graph().Pin()
			return pin != nil && string(pin.Status) == status
		}
	}
	nextUp := mustReplace(t, string(cujFixture(t)), `"status": "active",
      "depends_on"`, `"status": "pending",
      "depends_on"`)
	allDone := mustReplace(t, mustReplace(t, string(cujFixture(t)), `"status": "active",
      "depends_on"`, `"status": "done",
      "depends_on"`), `"status": "pending"`, `"status": "skipped"`)
	write := func(a *app) {
		other := filepath.Join(a.plans.Root, "cuj", "a-much-longer-plan-name-from-the-agent.rex.json")
		os.WriteFile(other, []byte(`{"version": 1, "tasks": []}`), 0o644)
		a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + other, Meta: map[string]any{"tool": "Write", "path": other, "ok": true}}})
	}
	return []matrixScreen{
		// Plans (docs/ux.md §11): a first run, with the skill missing and
		// installed, plans off and outside git; back from a plan, on
		// Continue; every kind of plan under Recent; Find; Open a plan and
		// a folder in it; New plan, empty, named and refused.
		{"plans, first run", true, func(a *app) {}, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.total == 0 && len(a.plans.Rows) == 1 && a.plans.Current == ""
		}},
		{"plans, first run, skill installed", true, func(a *app) {
			home, _ := os.UserHomeDir()
			for _, dir := range skill.Dirs(home, a.plans.Repo, true) {
				if err := skill.Install(dir, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
		}, func(a *app) bool { return lib(ScreenHome)(a) && skillHint(a.skill.look(a.plans.Repo, a.now())) == "" }},
		{"plans off", true, func(a *app) {
			os.WriteFile(filepath.Join(a.plans.Repo, "rex"), []byte("x"), 0o644)
			a.plans.Rebuild()
		}, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Notice == rexFileNotice && len(a.plans.Rows) == 0
		}},
		{"plans, outside git", true, func(a *app) {
			*a = *newApp("yellow", false, "idle", NewPlanBrowser(t.TempDir()), 120, 32) // no .git
		}, func(a *app) bool {
			return lib(ScreenHome)(a) && !a.plans.inGit && a.plans.Rows[a.plans.Cursor].Kind == PlanRowNew
		}},
		{"plans, back from a plan", false, list, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowContinue
		}},
		// Every kind of plan under Recent: a finished, an empty and an
		// unreadable plan, and long names (docs/ux.md §11).
		{"plans, every kind of plan", false, func(a *app) { seedKinds(t, a); list(a); press(a, "end") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan && a.plans.rowOf(PlanRowPlan) > 0
		}},
		{"plans, on Open latest", false, func(a *app) {
			seedKinds(t, a)
			newest := time.Now().Add(time.Hour)
			os.Chtimes(filepath.Join(a.plans.Root, "ux", "redesign.rex.json"), newest, newest)
			list(a)
			pick(a, PlanRowLatest)
		}, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowLatest
		}},
		{"find", false, func(a *app) { list(a); typeText(a, "send") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan && a.fieldOpen()
		}},
		{"find, no match", false, func(a *app) { list(a); typeText(a, "zzz-nothing") }, func(a *app) bool {
			return lib(ScreenHome)(a) && len(a.plans.Rows) == 1 && a.plans.Rows[0].Kind == PlanRowCreateNamed
		}},
		{"open a plan", false, browse, func(a *app) bool {
			return lib(ScreenBrowse)(a) && a.plans.topFolder() && a.plans.Rows[a.plans.Cursor].Kind == PlanRowDir
		}},
		{"open a plan, a folder", false, func(a *app) { seedKinds(t, a); browse(a); press(a, "\r") }, func(a *app) bool {
			return lib(ScreenBrowse)(a) && !a.plans.topFolder() && a.plans.Rows[a.plans.Cursor].Kind == PlanRowPlan
		}},
		{"open a plan, filtered", false, func(a *app) { seedKinds(t, a); browse(a); typeText(a, "old") }, func(a *app) bool {
			return lib(ScreenBrowse)(a) && a.fieldOpen() && a.plans.Filter == "old"
		}},
		{"new plan", false, func(a *app) { list(a); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x/y") }, lib(ScreenCreate)},
		{"new plan, empty", false, func(a *app) { list(a); pick(a, PlanRowNew); press(a, "\r") }, lib(ScreenCreate)},
		{"new plan, a name taken", false, func(a *app) {
			list(a)
			pick(a, PlanRowNew)
			press(a, "\r")
			typeText(a, "cuj/sendoff-demo")
			press(a, "\r")
		}, func(a *app) bool {
			return lib(ScreenCreate)(a) && a.plans.Err != "" && noticeKindOf(a) == noticeError
		}},
		// The task list (docs/ux.md §6): on the agent's pin, on what Rex
		// would pin, with nothing to pin, on a done row, with groups, long
		// enough to scroll (↑ N more on row 2), and a plan with problems.
		{"tasks, on the pin", false, func(a *app) {}, pinned("active")},
		{"tasks, next up", false, rewrite(nextUp), pinned("pending")},
		{"tasks, no pin", false, rewrite(allDone), func(a *app) bool {
			return at(atOutline)(a) && a.doc.plan != nil && a.doc.plan.Graph().Pin() == nil && len(a.doc.plan.Tasks) == 3
		}},
		{"tasks, a done row", false, func(a *app) { press(a, "up") }, func(a *app) bool { return at(atOutline)(a) && a.outline.sel == "plans-ui" }},
		{"tasks, cursor off the pin", false, func(a *app) { press(a, "down") }, func(a *app) bool { return at(atOutline)(a) && a.outline.sel == "load-outline" }},
		{"tasks, groups", false, func(a *app) { rewrite(redesignPlan)(a); press(a, "down", "down") }, func(a *app) bool {
			return pinned("active")(a) && a.outline.sel == "plan-list"
		}},
		{"tasks, scrolled", false, func(a *app) { rewrite(manyTasks(80))(a); press(a, "end") }, func(a *app) bool {
			return at(atOutline)(a) && a.outline.sel == "t79" && a.outline.view > 0
		}},
		{"tasks, read-only with problems", false, rewrite(`{"version": 2, "goal": "Two active tasks, a newer version, problems", "tasks": [{"id": "a", "title": "A", "status": "active", "does": ["x"]}, {"id": "b", "title": "B", "status": "active", "depends_on": ["zz"]}]}`), func(a *app) bool {
			return pinned("active")(a) && a.doc.plan.ReadOnly != "" && len(a.doc.plan.Problems) > 0
		}},
		// An agent's first save: the pin has only a title (docs/ux.md §6).
		{"tasks, bare pin", false, rewrite(`{"version": 1, "title": "Fresh", "goal": "First save", "tasks": [{"id": "a", "title": "First thing", "status": "active"}, {"id": "b", "title": "Second thing", "depends_on": ["a"]}, {"id": "c"}]}`), func(a *app) bool {
			return pinned("active")(a) && len(a.doc.plan.Graph().Pin().Does) == 0
		}},
		// No task rows: an empty read-only plan with problems.
		{"tasks, empty read-only plan with problems", false, rewrite(`{"version": 2, "title": "Newer", "goal": "a goal", "status": "weird", "tasks": []}`), func(a *app) bool {
			return at(atOutline)(a) && a.doc.plan != nil && a.doc.plan.ReadOnly != "" && len(a.doc.plan.Problems) > 1 && len(a.doc.plan.Tasks) == 0
		}},
		// A plan your agent is writing: its write opened it (following).
		{"tasks, being written", false, func(a *app) {
			p := filepath.Join(a.plans.Root, "auth", "reset.rex.json")
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(`{"version": 1, "title": "Password reset", "tasks": [{"id": "token", "title": "Reset token table"}, {"id": "email", "title": "Send the reset email", "depends_on": ["token"]}]}`), 0o644)
			a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p, "ok": true}}})
		}, func(a *app) bool {
			return at(atOutline)(a) && a.follow && filepath.Base(a.plans.Current) == "reset.rex.json" && noticeKindOf(a) == noticeToast
		}},
		// A parent loop: its tasks are rows at the top, tagged cycle.
		{"tasks, a parent loop", false, rewrite(`{"version": 1, "title": "Loop", "tasks": [{"id": "a", "title": "A", "parent": "b"}, {"id": "b", "title": "B", "parent": "a"}]}`), func(a *app) bool {
			return at(atOutline)(a) && a.outline.sel == "a" && len(taskKeys(a.doc.plan)) == 2
		}},
		// A plan that has never parsed: nothing to show but why.
		{"tasks, unreadable", false, func(a *app) {
			p := filepath.Join(a.plans.Root, "ux", "broken.rex.json")
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(`{"version": 1, "tasks": [`), 0o644)
			a.plans.open(p)
			a.openedPlan()
		}, func(a *app) bool { return at(atOutline)(a) && a.doc.plan == nil }},
		// The notice row's kinds (docs/ux.md §4): hard drift, a plan the
		// file no longer parses (its last good copy shows), the plan list's
		// error; your agent asking, a toast, an offer and a held plan below.
		{"tasks, hard drift", false, func(a *app) {
			p := filepath.Join(a.plans.Repo, "README.md")
			a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "wrote " + p, Meta: map[string]any{"tool": "Write", "path": p, "ok": true}}})
		}, func(a *app) bool { k := noticeKindOf(a); return at(atOutline)(a) && k == noticeDrift }},
		{"tasks, can't read, last good copy", false, func(a *app) {
			os.WriteFile(a.plans.Current, []byte("{"), 0o644)
			a.reloadDoc()
			a.reloadDoc()
		}, func(a *app) bool { return at(atOutline)(a) && a.doc.plan != nil && noticeKindOf(a) == noticeError }},
		{"plans, an error", false, func(a *app) { list(a); typeText(a, "/nope"); press(a, "\r") }, func(a *app) bool {
			return lib(ScreenHome)(a) && a.plans.Err != "" && noticeKindOf(a) == noticeError
		}},
		{"tasks, your agent asks", false, func(a *app) {
			a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
		}, func(a *app) bool { return at(atOutline)(a) && a.needs == "ask" }},
		{"held attach", false, func(a *app) {
			list(a)
			typeText(a, "zz")
			press(a, "\t") // Plans' find holds it, seen from AGENT
			write(a)
		}, func(a *app) bool { return a.pane == PaneChat && a.pending != "" }},
		{"new plan offer", false, func(a *app) {
			list(a)
			typeText(a, "scratch")
			press(a, "\r") // made on Plans: latched
			write(a)
		}, func(a *app) bool { return at(atOutline)(a) && a.offer != "" && !a.follow }},
		// Details on a page of their own (docs/ux.md §6): the pin's, a
		// pending task's and a done task's; About on i.
		{"task details, the pin", false, func(a *app) { press(a, "\r") }, func(a *app) bool { return at(atTaskDetail)(a) && a.outline.sel == "card-highlight" }},
		{"task details, a pending task", false, func(a *app) { press(a, "down", "\r") }, func(a *app) bool { return at(atTaskDetail)(a) && a.outline.sel == "load-outline" }},
		{"task details, a done task", false, func(a *app) { press(a, "up", "\r") }, func(a *app) bool { return at(atTaskDetail)(a) && a.outline.sel == "plans-ui" }},
		{"task details, in a group", false, func(a *app) { rewrite(redesignPlan)(a); press(a, "home", "\r") }, func(a *app) bool {
			return at(atTaskDetail)(a) && a.outline.sel == "tab-bar"
		}},
		{"about", false, func(a *app) { press(a, "i") }, at(atPlanDetail)},
		{"agent", false, func(a *app) {
			press(a, "\t")
			a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "a reply with 中文 and ⚠️ in it \x1b[2J"}})
		}, func(a *app) bool { return a.pane == PaneChat && a.overlay[a.pane] == overlayNone }},
		{"agent, empty", true, func(a *app) { press(a, "\t") }, func(a *app) bool { return a.pane == PaneChat && len(a.log) == 0 }},
		// A first run: every harness found, none wired, the skill missing,
		// so the feed's health is taller than most bodies (docs/ux.md §10).
		{"agent, empty, first run", true, func(a *app) {
			a.health, a.hidden = notWired(), 4
			press(a, "\t")
		}, func(a *app) bool { return a.pane == PaneChat && len(a.log) == 0 && len(a.health.statuses) == 4 }},
		{"agent, thinking live", false, func(a *app) {
			press(a, "\t")
			a.ingest(agentTurn(a)[:7])
		}, func(a *app) bool { return a.pane == PaneChat && a.viewOpt().live && a.lastKind == "thought" }},
		{"agent, a whole turn", false, func(a *app) {
			press(a, "\t")
			a.ingest(agentTurn(a))
		}, func(a *app) bool { return a.pane == PaneChat && a.turn == TurnIdle && len(a.drift.items) > 0 }},
		{"agent, asks", false, func(a *app) {
			press(a, "\t")
			a.ingest(agentTurn(a)[:len(agentTurn(a))-1])
		}, func(a *app) bool { return a.pane == PaneChat && a.lastKind == "question" }},
		{"agent, scrolled", false, func(a *app) {
			press(a, "\t")
			var earlier []watch.Event
			for i := 0; i < 20; i++ {
				earlier = append(earlier, watch.NewEvent("claude", "text", fmt.Sprintf("reply %d", i), nil))
			}
			a.ingest(append(earlier, agentTurn(a)...))
			press(a, "up", "up", "up")
		}, func(a *app) bool { return a.pane == PaneChat && a.scroll > 0 }},
		{"agent, warm start", false, func(a *app) {
			// Short enough that the rule is on screen at every size, so
			// the frame checks cover it.
			evs := agentTurn(a)
			a.warmStart([]watch.Event{evs[5], evs[6], evs[13], evs[14]}, time.Now())
			press(a, "\t")
		}, func(a *app) bool {
			if a.pane != PaneChat || len(a.log) == 0 || a.log[0].Kind != "before" {
				return false
			}
			for _, r := range a.frameRows().Rows {
				if strings.Contains(textOf(r, a.columns), "── before rex started") {
					return true
				}
			}
			return false
		}},
		{"tasks, empty", false, func(a *app) { list(a); typeText(a, "scratch"); press(a, "\r") }, func(a *app) bool {
			return at(atOutline)(a) && len(a.doc.plan.Tasks) == 0
		}},
		{"help", false, func(a *app) { press(a, "\t", "?") }, over(overlayHelp)},
		{"help over Plans", false, func(a *app) { list(a); press(a, "?") }, func(a *app) bool { return over(overlayHelp)(a) && a.at == atLibrary }},
		{"help over a task", false, func(a *app) { press(a, "\r", "?") }, func(a *app) bool { return over(overlayHelp)(a) && a.at == atTaskDetail }},
	}
}

// noticeKindOf is which kind of notice a's notice row shows.
func noticeKindOf(a *app) noticeKind {
	_, k := a.noticeOf()
	return k
}

// matrixSizes are the frame matrix's terminal sizes (AGENTS.md, Commands
// and tests): around every tier of docs/ux.md §12, the places' label's
// 30 cells, 38 cells, either side of the mascot's 110×16, and past the
// text measure.
var matrixSizes = [][2]int{{24, 7}, {24, 12}, {24, 24}, {30, 10}, {38, 7}, {38, 24}, {44, 16}, {80, 7}, {80, 10}, {80, 12}, {80, 13}, {80, 16}, {80, 20}, {80, 24}, {109, 15}, {109, 24}, {110, 15}, {110, 16}, {120, 7}, {120, 12}, {120, 24}, {120, 32}, {200, 60}}

// footerWords are the keys a footer segment starts with (docs/ux.md §4).
var footerWords = map[string]bool{"↑↓": true, "enter": true, "esc": true, "tab": true, "?": true, "home": true, "end": true, "type": true, "ctrl+c": true, "i": true}

// seedKinds adds a plan of every kind to the CUJ repo (docs/ux.md §11):
// more root plans than RECENT holds, written first, so FOLDERS lists
// some; then the newest, which lead RECENT under the open CUJ plan.
// writePlan's clock stamps each a second after the last.
func seedKinds(t *testing.T, a *app) {
	repo := a.plans.Repo
	for i := 0; i < 4; i++ {
		writePlan(t, repo, fmt.Sprintf("old-%d.rex.json", i), `{"version": 1, "title": "An older plan", "tasks": [{"id": "x"}]}`)
	}
	writePlan(t, repo, "ux/redesign.rex.json", `{"version": 1, "title": "Rex visual redesign 中文 ⚠️", "goal": "One look \u001b[2J everywhere", "status": "active", "tasks": [{"id": "tabs", "status": "done", "accomplished": ["tabs \u001b[31m on row 0"]}, {"id": "stream", "title": "Agent stream", "status": "active"}, {"id": "list", "title": "Plan list", "depends_on": ["stream"]}, {"id": "outline"}]}`)
	writePlan(t, repo, "done.rex.json", `{"version": 1, "title": "Shipped", "status": "done", "tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "skipped"}]}`)
	writePlan(t, repo, "empty.rex.json", `{"version": 1, "title": "Nothing yet", "tasks": []}`)
	writePlan(t, repo, "broken.rex.json", `{"version": 1, "tasks": [`)
	a.plans.walk = planWalk{}
}

// agentTurn is a realistic turn on the CUJ plan (docs/ux.md §10): a
// prompt, a run of thoughts, more commands than a run shows, a failed step,
// an edit off the pin, the turn's error, an ask and a reply.
func agentTurn(a *app) []watch.Event {
	repo := a.plans.Repo
	evs := []watch.Event{{Source: "claude", Kind: "user", Text: "make the selected card one block, with the bar and the fill on all three rows of it"}}
	for _, s := range []string{"Looking at the plans view first.", "The border is drawn apart from the fill.", "So two painters.", "One painter, then.", "Check live.go too.", "The bar is drawn per row, so the fill has to start at column 0 to cover the gutter and the border cell."} {
		evs = append(evs, watch.Event{Source: "claude", Kind: "thought", Text: s})
	}
	for _, s := range []string{"Edit internal/tui/plans.go", "Edit internal/tui/live.go", "go vet ./...", "Edit internal/tui/plans_test.go", "Edit internal/tui/outline.go", "gofmt -l .", "go test ./internal/tui -run PlanCardSelection"} {
		evs = append(evs, watch.Event{Source: "claude", Kind: "command", Text: s, Meta: map[string]any{"tool": "Bash", "command": s}})
	}
	evs = append(evs,
		watch.Event{Source: "claude", Kind: "error", Text: "Bash failed · go test ./internal/tui -run PlanCardSelection: exit status 1", Meta: map[string]any{"tool": "Bash", "command": "go test ./internal/tui -run PlanCardSelection"}},
		watch.Event{Source: "claude", Kind: "result", Text: "wrote README.md", Meta: map[string]any{"tool": "Write", "path": filepath.Join(repo, "README.md"), "ok": true}},
		watch.Event{Source: "claude", Kind: "error", Text: "turn failed: the model hit its context limit; the turn ended"},
		watch.Event{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)? It runs every package."},
		watch.Event{Source: "claude", Kind: "text", Text: "## Done\n\nThe selected card is one block now.\n\n- plans.go draws the card through one painter\n- live.go starts the fill at column 0\n\n```go\nfill(0)\n```"},
	)
	now := time.Now()
	for i := range evs {
		evs[i].TS = float64(now.Add(time.Duration(i-len(evs))*time.Second).UnixNano()) / 1e9
	}
	return evs
}

// matrixApp is a fresh app on screen s at cols×lines.
func matrixApp(t *testing.T, s matrixScreen, cols, lines int) *app {
	var a *app
	if s.empty {
		a = testApp(t) // Plans' first run
	} else {
		a, _, _ = cujApp(t)
	}
	s.setup(a)
	a.settle(cols, lines)
	return a
}

// Every screen fits every size: no row wider than the terminal, no control
// bytes from content, row 0 a title row (its crumbs from column 0, the
// places' label at its end from 30 cells), row 2 blank (but for ↑ N more
// at its right end) and a notice row from 12 rows, and a footer of " · "-
// joined key words that says where to go (docs/ux.md §4). No box is empty,
// and the task list never shows the plan list (§6).
func TestFrameMatrix(t *testing.T) {
	for _, size := range matrixSizes {
		for _, s := range matrixScreens(t) {
			a := matrixApp(t, s, size[0], size[1])
			f := a.frameRows()
			name := s.name + " " + strings.Join([]string{strconv.Itoa(size[0]), strconv.Itoa(size[1])}, "×")
			if !s.reached(a) {
				t.Errorf("%s: setup didn't reach the screen", name)
				continue
			}
			if len(f.Rows) != size[1] {
				t.Errorf("%s: %d rows", name, len(f.Rows))
				continue
			}
			textW := PaneTextWidth(size[0], size[1])
			last := len(f.Rows) - 1
			for i, row := range f.Rows {
				if cellLen(row) > size[0] {
					t.Errorf("%s: row %d is %d cells", name, i, cellLen(row))
				}
				// Rex has no verbs, and keys are words: no footer,
				// placeholder or copy offers a verb, an icon for a key, the
				// old tab bar's pills, or the old name for AGENT (docs/ux.md
				// §4, §6).
				for _, never := range []string{"⏎", "↔", "tape", "brief", "send-off", "clipboard", "[ + ", "[PLAN]", "[AGENT]"} {
					if strings.Contains(row, never) {
						t.Errorf("%s: row %d has %q: %q", name, i, never, row)
					}
				}
				// The old selection bar: never in the chrome or the gutter
				// (plan text may hold one).
				if text := textOf(row, size[0]); strings.HasPrefix(text, "▌") || (i < 2 || i >= last-1) && strings.Contains(text, "▌") {
					t.Errorf("%s: row %d has ▌: %q", name, i, text)
				}
				for _, r := range row {
					if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
						t.Errorf("%s: control %U in row %d", name, r, i)
					}
				}
			}
			for i := 1; i < len(f.Rows); i++ {
				if strings.HasPrefix(textOf(f.Rows[i-1], size[0]), "╭") && strings.HasPrefix(textOf(f.Rows[i], size[0]), "╰") {
					t.Errorf("%s: an empty box on rows %d and %d", name, i-1, i)
				}
			}
			top, bottom := 2, last
			if size[1] >= blankFrom {
				top, bottom = 3, last-1
			}
			// The task list never shows the plan list's rows, even with an
			// empty body.
			if a.pane == PanePlans && a.overlay[a.pane] == overlayNone && a.at == atOutline && bottom > top {
				var body []string
				for _, r := range f.Rows[top:bottom] {
					body = append(body, textOf(r, size[0]))
				}
				for _, never := range []string{"+ New plan", "no tasks yet · the", "OPEN ─", "RECENT ─", "FOLDERS ─"} {
					if b := strings.Join(body, "\n"); strings.Contains(b, never) {
						t.Errorf("%s: the plan list's %q under an open plan's title\n%s", name, never, b)
					}
				}
			}
			// Row 0 is a title row: its crumbs from column 0, no tab bar,
			// and from 30 cells the places' label at its end, whole or the
			// place on screen.
			head := textOf(f.Rows[0], size[0])
			here := placeWords[a.pane]
			switch {
			case head == "" || strings.HasPrefix(head, " ") || strings.HasPrefix(head, "REX"):
				t.Errorf("%s: row 0 isn't a title: %q", name, head)
			case textW >= labelFrom && !strings.HasSuffix(head, "plan · agent") && !strings.HasSuffix(head, " "+here):
				t.Errorf("%s: row 0 has no places' label: %q", name, head)
			case a.overlay[a.pane] != overlayNone && !strings.Contains(head, "Help"):
				t.Errorf("%s: Help's title %q", name, head)
			}
			if row2 := textOf(f.Rows[2], size[0]); size[1] >= blankFrom && row2 != "" && !moreUp.MatchString(row2) {
				t.Errorf("%s: row 2 isn't blank: %q", name, row2)
			}
			footer := textOf(f.Rows[last], size[0])
			for _, s := range strings.Split(footer, footerSep) {
				if key, _, _ := strings.Cut(s, " "); !footerWords[key] || strings.Contains(s, "  ") || strings.Contains(s, "·") {
					t.Errorf("%s: footer %q: segment %q", name, footer, s)
				}
			}
			if textW >= 38 && a.overlay[a.pane] == overlayNone && !strings.Contains(footer, "tab") && !strings.Contains(footer, "esc") {
				t.Errorf("%s: footer doesn't say where to go: %q", name, footer)
			}
		}
	}
}

// moreUp is row 2 while the open plan's list or page is scrolled: blank
// but for ↑ N more at its right end, a cell short of the edge.
var moreUp = regexp.MustCompile(`^ +↑ [0-9]+ more$`)

// docs/ux.md §5, rule 4: color never changes the characters. Every
// screen's text column in color, stripped of its SGR, is its NO_COLOR
// frame.
func TestColorFrameSameText(t *testing.T) {
	for _, size := range [][2]int{{38, 24}, {80, 24}, {120, 24}, {80, 10}} {
		for _, s := range matrixScreens(t) {
			a := matrixApp(t, s, size[0], size[1])
			plain := a.frameRows().Rows
			a.enabled = true
			color := a.frameRows().Rows
			l := a.layout()
			for i := range plain {
				if got, want := tailCells(stripSGR(color[i]), l.textW), tailCells(plain[i], l.textW); got != want {
					t.Errorf("%s %d×%d row %d:\ncolor    %q\nno color %q", s.name, size[0], size[1], i, got, want)
				}
			}
			if !strings.Contains(strings.Join(color, ""), "\033[1m") {
				t.Errorf("%s: no bold anywhere", s.name)
			}
		}
	}
}

// docs/ux.md §5: quiet roles on every screen of the matrix. The text
// column never paints a background (the mascot's half-block pixels are
// its own); every ✔ is done's green; yellow is only on the pin's marks
// (its pinned tag, its id on row 1 and on AGENT, its state on a details
// page's row 0) and hard drift's "off pin"; purple is only the selection,
// one block from its ❯; and a list screen with a row to select has
// exactly one ❯, the rest none. Row 0 and the footer: the current crumb
// bold, the footer muted.
func TestFrameMatrixQuietRoles(t *testing.T) {
	green, purple, yellow := mascot.FG(mascot.UI["action"]), mascot.FG(mascot.UI["pick"]), mascot.FG(mascot.UI["accent"])
	pinMark := func(row string) bool {
		trimmed := strings.TrimSpace(row)
		for _, mark := range []string{"▸ ", "[PINNED]", "pinned", "hard", "off pin"} {
			if strings.Contains(row, mark) {
				return true
			}
		}
		return strings.HasPrefix(trimmed, "│") || strings.HasPrefix(trimmed, "╰")
	}
	muted := mascot.FG(mascot.UI["muted"])
	for _, size := range matrixSizes {
		for _, s := range matrixScreens(t) {
			a := matrixApp(t, s, size[0], size[1])
			name := fmt.Sprintf("%s %d×%d", s.name, size[0], size[1])
			a.enabled = true
			marks, block, from := 0, false, -1
			help := a.overlay[a.pane] != overlayNone
			column := textColumn(a.layout(), a.lines, a.composeOptions())
			if footer := column[len(column)-1]; strings.TrimSpace(stripSGR(footer)) != "" && (!strings.HasPrefix(footer, muted) || strings.Contains(footer, "\033[1m")) {
				t.Errorf("%s: the footer isn't muted: %q", name, footer)
			}
			if !strings.Contains(column[0], "\033[1m") {
				t.Errorf("%s: row 0's crumb isn't bold: %q", name, column[0])
			}
			for i, row := range column {
				if strings.Contains(row, yellow) && !help && !pinMark(stripSGR(row)) {
					t.Errorf("%s: row %d is yellow and isn't the pin: %q", name, i, stripSGR(row))
				}
				if strings.Contains(row, "\033[48") {
					t.Errorf("%s: row %d paints a background: %q", name, i, row)
				}
				for at := 0; ; {
					j := strings.Index(row[at:], doneMark)
					if j < 0 {
						break
					}
					if head := strings.TrimSuffix(row[:at+j], "\033[1m"); !strings.HasSuffix(head, green) {
						t.Errorf("%s: row %d has a ✔ that isn't green: %q", name, i, row)
					}
					at += j + len(doneMark)
				}
				if n := strings.Count(stripSGR(row), "❯"); n > 0 {
					marks += n
					block, from = true, i
				}
				switch picked := strings.Contains(row, purple); {
				case picked && !block:
					t.Errorf("%s: row %d is purple off the selection: %q", name, i, stripSGR(row))
				case !picked:
					block = false
				}
			}
			list := !help && a.pane == PanePlans && (a.at == atOutline && a.outline.sel != "" || a.at == atLibrary && len(a.plans.Rows) > 0 && !a.plans.Prompting())
			switch {
			case help:
			case list && marks != 1:
				t.Errorf("%s: a list screen has %d ❯ (from row %d)\n%s", name, marks, from, stripSGR(strings.Join(a.frameRows().Rows, "\n")))
			case !list && marks != 0:
				t.Errorf("%s: %d ❯ on a screen with no list", name, marks)
			}
		}
	}
}
