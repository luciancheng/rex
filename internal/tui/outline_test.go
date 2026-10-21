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
)

const demoPlan = `{
  "version": 1,
  "id": "sendoff-demo",
  "title": "Send-off demo",
  "goal": "Show a plan, pin a task, and send it off.",
  "status": "active",
  "scope": {"in": ["outline view"], "out": ["extra palettes"]},
  "tasks": [
    {
      "id": "plans-ui",
      "title": "Plans navigator",
      "status": "done",
      "does": ["Create, open, browse rex/plans in the TUI"],
      "accomplished": ["Home cards, browse, duplicate warn"],
      "modified": ["internal/tui/plans.go"]
    },
    {
      "id": "card-highlight",
      "title": "Selected plan card is one block",
      "status": "active",
      "depends_on": ["plans-ui"],
      "needs": ["pick / pickbg colors exist"],
      "does": ["Bar, border, and fill cover the whole selected card"],
      "verify": ["Open latest: ▌ on all three card rows"],
      "files": ["internal/tui/plans.go"]
    },
    {
      "id": "load-outline",
      "title": "Outline view",
      "status": "pending",
      "depends_on": ["card-highlight"],
      "does": ["Show tasks"]
    }
  ]
}
`

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// openDemo opens src as cuj/demo.rex.json in a fresh app at cols×rows.
func openDemo(t *testing.T, src string, cols, rows int) (*app, *clock, string) {
	t.Helper()
	a := testApp(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	a.columns, a.lines = cols, rows
	path := filepath.Join(a.plans.Root, "cuj", "demo.rex.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	typeText(a, "cuj/demo")
	press(a, "\r")
	if a.at != atOutline || a.doc == nil {
		t.Fatalf("not on the outline: at=%d %q", a.at, a.plans.Err)
	}
	return a, c, path
}

// later moves the clock past a poll and ticks.
func later(a *app, c *clock) {
	c.t = c.t.Add(docPollEvery + time.Millisecond)
	a.tick()
}

func TestOutlineAtAGlance(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	frame := a.frame()
	for _, want := range []string{
		"REX plan",
		"rex/plans/cuj/demo.rex.json",
		"▸ card-highlight  Selected plan card is one block (active)",
		"── Send-off demo · active · 1/3 ──",
		"last ✓ plans-ui: Home cards, browse, duplicate warn",
		"✓ plans-ui",
		"▌ ▸ card-highlight",
		"Selected plan card is one block · Bar, border, and fill",
		"· load-outline",
		"after card-highlight",
		"outline · tab tape · esc plans · ⏎ detail · ?⏎ help",
		"?⏎ help",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	if a.outline.sel != "card-highlight" {
		t.Fatalf("the pin is selected on open, got %q", a.outline.sel)
	}
}

func TestOutlineNarrowShowsTheBlurbUnderTheSelection(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 80, 24)
	frame := a.frame()
	if strings.Contains(frame, "Plans navigator · Create") {
		t.Fatal("rows are title-only below 60 columns")
	}
	if !strings.Contains(frame, "▌ ▸ card-highlight Selected") || !strings.Contains(frame, "▌   Bar, border") || !strings.Contains(frame, "▸ card-highlight (active)") {
		t.Fatalf("the selected row's blurb is on its own line\n%s", frame)
	}
	for _, row := range a.frameRows().Rows {
		if cellLen(row) > 80 {
			t.Fatalf("row overflows: %q", row)
		}
	}
}

func TestOutlineDetailAndBack(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "\r")
	if a.at != atTaskDetail {
		t.Fatal("⏎ on a task opens its detail")
	}
	frame := a.frame()
	for _, want := range []string{"── card-highlight · active · pinned", "after", "✓ plans-ui: Plans navigator", "then", "load-outline: Outline view", "needs", "pick / pickbg colors exist", "verify", "files", "detail · esc outline"} {
		if !strings.Contains(frame, want) {
			t.Errorf("task detail missing %q\n%s", want, frame)
		}
	}
	press(a, "esc")
	if a.at != atOutline || a.outline.sel != "card-highlight" {
		t.Fatal("Esc goes back to the outline, selection kept")
	}
	press(a, "up", "up", "up")
	if a.outline.sel != "" {
		t.Fatalf("the title rule is the top row, got %q", a.outline.sel)
	}
	press(a, "\r")
	frame = a.frame()
	for _, want := range []string{"── plan · Send-off demo", "goal", "Show a plan, pin a task", "in scope", "out of scope", "extra palettes"} {
		if !strings.Contains(frame, want) {
			t.Errorf("plan detail missing %q\n%s", want, frame)
		}
	}
	press(a, "left")
	press(a, "down", "right")
	if a.at != atTaskDetail || a.outline.sel != "plans-ui" {
		t.Fatal("→ opens task detail")
	}
	if frame := a.frame(); !strings.Contains(frame, "retrospective") || !strings.Contains(frame, "internal/tui/plans.go") {
		t.Fatalf("a done task shows its retrospective\n%s", frame)
	}
	press(a, "esc", "esc")
	if a.at != atLibrary || a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue {
		t.Fatalf("Esc from the outline lands on Continue: at=%d row=%+v", a.at, a.plans.Rows[a.plans.Cursor])
	}
	if !strings.Contains(a.frame(), "rex/plans/cuj/demo.rex.json") {
		t.Fatal("the plan stays open in the header")
	}
	press(a, "\r")
	if a.at != atOutline {
		t.Fatal("Continue reopens the outline")
	}
}

func TestOutlineTabKeepsItsScreen(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "down", "\r", "\t")
	if a.pane != PaneChat {
		t.Fatal("tab goes to the tape")
	}
	if !strings.Contains(a.frame(), "▸ card-highlight") {
		t.Fatal("row 1 shows the pin on the tape too")
	}
	press(a, "\t")
	if a.at != atTaskDetail || a.outline.sel != "load-outline" {
		t.Fatal("tab back returns to the same detail")
	}
}

func TestOutlineReloadsOutsideEdits(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	edited := strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)
	edited = strings.Replace(edited, "  ]\n}", `    ,{"id": "extra", "title": "Extra"}
  ]
}`, 1)
	os.WriteFile(path, []byte(edited), 0o644)
	later(a, c)
	line := a.statusLine().Text
	if line != "plan updated · load-outline → skipped · +1 task" {
		t.Fatalf("toast %q", line)
	}
	if a.outline.sel != "load-outline" {
		t.Fatal("the selection stays on the same task")
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "load-outline → skipped · +1 task" {
		t.Fatalf("tape line %+v", last)
	}
	if !strings.Contains(a.frame(), "– load-outline") || !strings.Contains(a.frame(), "extra") {
		t.Fatal("the outline shows the new file")
	}
	c.t = c.t.Add(toastFor)
	a.tick()
	if a.statusLine().Text != "▸ card-highlight  Selected plan card is one block (active)" {
		t.Fatalf("the toast expires: %q", a.statusLine().Text)
	}
	// Rex's own write is taken in quietly.
	own := strings.Replace(edited, `"title": "Extra"`, `"title": "Extra", "status": "done"`, 1)
	a.doc.own = plan.Hash([]byte(own))
	os.WriteFile(path, []byte(own), 0o644)
	logs := len(a.log)
	later(a, c)
	if len(a.log) != logs || strings.HasPrefix(a.statusLine().Text, "plan updated") {
		t.Fatal("Rex's own write must not toast")
	}
}

func TestOutlineInvalidJSONIsDebounced(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	os.WriteFile(path, []byte(demoPlan[:200]), 0o644)
	later(a, c)
	if strings.Contains(a.statusLine().Text, "error") {
		t.Fatal("one poll of bad JSON is an agent mid-save")
	}
	later(a, c)
	if got := a.statusLine().Text; !regexp.MustCompile(`^plan JSON error \d+:\d+ · showing last good$`).MatchString(got) {
		t.Fatalf("status %q", got)
	}
	if !strings.Contains(a.frame(), "▸ card-highlight") || a.doc.writable() == "" {
		t.Fatal("the last good plan stays on screen, not writable")
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if strings.Contains(a.statusLine().Text, "error") {
		t.Fatal("a fixed file clears the error")
	}
	os.Remove(path)
	later(a, c)
	if a.statusLine().Text != "plan file missing · showing last good" {
		t.Fatalf("%q", a.statusLine().Text)
	}
}

func TestOutlineStates(t *testing.T) {
	a, _, _ := openDemo(t, `{"version": 2, "tasks": [{"id": "a", "title": "A"}]}`, 120, 32)
	frame := a.frame()
	if !strings.Contains(frame, "read-only: version 2") || !strings.Contains(frame, "outline · read-only · esc plans") {
		t.Fatalf("%s", frame)
	}
	b, _, _ := openDemo(t, `{"tasks": [{"id": "a", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]}]}`, 120, 32)
	frame = b.frame()
	for _, want := range []string{"after ?zz", "cycle", "! 2 problems · ⏎ on the title lists them", "no pin · nothing unblocked"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	c, _, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done"}]}`, 120, 32)
	if c.statusLine().Text != "no pin · all tasks done" {
		t.Fatal(c.statusLine().Text)
	}
	d, _, _ := openDemo(t, `{"tasks": [`, 120, 32)
	if frame := d.frame(); !strings.Contains(frame, "can't read this plan") || !strings.HasPrefix(d.statusLine().Text, "plan JSON error 1:") {
		t.Fatalf("%q\n%s", d.statusLine().Text, frame)
	}
	e, _, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "active"}, {"id": "b", "status": "active"}]}`, 120, 32)
	if e.statusLine().Text != "2 active: a, b · pinned a" {
		t.Fatal(e.statusLine().Text)
	}
	f, _, _ := openDemo(t, `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "x", "parent": "ui", "status": "done"}, {"id": "y", "parent": "ui"}]}`, 120, 32)
	if frame := f.frame(); !strings.Contains(frame, "1/2") || f.outline.sel != "y" {
		t.Fatalf("a parent shows a rollup and is never the pin (sel %q)\n%s", f.outline.sel, frame)
	}
}

func TestOutlineScrollsAndKeepsTheSelection(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d"}`, i, i))
	}
	a, c, path := openDemo(t, `{"title": "Big", "tasks": [`+strings.Join(tasks, ",")+`]}`, 120, 24)
	for i := 0; i < 11; i++ {
		press(a, "down")
	}
	frame := a.frame()
	if a.outline.sel != "t12" || !strings.Contains(frame, "tasks 12/40") || !strings.Contains(frame, "↓ more") || !strings.Contains(frame, "▌ ○ t12") {
		t.Fatalf("sel %q\n%s", a.outline.sel, frame)
	}
	press(a, "end")
	if a.outline.sel != "t40" || !strings.Contains(a.frame(), "▌ ○ t40") {
		t.Fatal("End goes to the last task")
	}
	os.WriteFile(path, []byte(`{"title": "Big", "tasks": [{"id": "new"},`+strings.Join(tasks, ",")+`]}`), 0o644)
	later(a, c)
	if a.outline.sel != "t40" || !strings.Contains(a.frame(), "▌ ○ t40") {
		t.Fatal("a reload keeps the selection by id")
	}
}

func TestOutlineTypingIsForVerbs(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	typeText(a, "zap")
	press(a, "\r")
	if a.statusLine().Text != "'zap' is not a verb · ?⏎ help" || a.draft != "zap" {
		t.Fatalf("%q draft %q", a.statusLine().Text, a.draft)
	}
	press(a, "esc")
	if a.draft != "" || a.at != atOutline {
		t.Fatal("Esc clears the draft first")
	}
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[PanePlans] != overlayHelp {
		t.Fatal("verbs run on the outline")
	}
}

func TestContinueCardAfterRestart(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	again := newApp("yellow", false, "idle", NewPlanBrowser(a.plans.Repo), 120, 32)
	row := again.plans.Rows[again.plans.Cursor]
	if row.Kind != PlanRowContinue || row.Path != path {
		t.Fatalf("cursor on %+v", row)
	}
	if !strings.Contains(again.frame(), "Continue cuj/demo") || !strings.Contains(again.frame(), "1/3 · ▸ card-highlight") {
		t.Fatalf("%s", again.frame())
	}
	if again.plans.Current != "" || strings.Contains(again.frame(), "· rex/plans/cuj/demo.rex.json") {
		t.Fatal("start does not open a plan by itself")
	}
}

func TestOutlineRuleStaysAsTheListScrolls(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d"}`, i, i))
	}
	a, _, _ := openDemo(t, `{"title": "Big", "tasks": [`+strings.Join(tasks, ",")+`]}`, 120, 24)
	for i := 0; i < 30; i++ {
		press(a, "down")
	}
	frame := a.frame()
	if !strings.Contains(frame, "── Big") || !strings.Contains(frame, "tasks 31/40") || !strings.Contains(frame, "↑ more") || !strings.Contains(frame, "▌ ○ t31") {
		t.Fatalf("deep in the list, the rule and its position stay on screen\n%s", frame)
	}
}

func TestContinueKeepsTheOpenPlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down") // load-outline
	sel := a.outline.sel
	press(a, "esc") // the Library, Continue selected
	os.WriteFile(path, []byte(`{"tasks": [`), 0o644)
	c.t = c.t.Add(docPollEvery + time.Millisecond)
	press(a, "\r")
	if a.at != atOutline || a.outline.sel != sel {
		t.Fatalf("Continue keeps the selection: at=%v sel=%q, want %q", a.at, a.outline.sel, sel)
	}
	if a.doc.plan == nil || a.doc.err != "" {
		t.Fatalf("a half-saved file right after Continue keeps the last good copy: err=%q", a.doc.err)
	}
}

func TestTaskDetailKeepsItsTaskWhenItLeaves(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down", "\r")
	if a.at != atTaskDetail || a.outline.sel != "load-outline" {
		t.Fatalf("at=%v sel=%q", a.at, a.outline.sel)
	}
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"id": "load-outline"`, `"id": "renamed"`, 1)), 0o644)
	later(a, c)
	if a.outline.sel != "load-outline" || !strings.Contains(a.frame(), "no longer in the file") {
		t.Fatalf("the detail says its task left instead of showing another\n%s", a.frame())
	}
	press(a, "esc")
	if a.outline.sel == "load-outline" {
		t.Fatal("back on the outline, the selection falls back to a task that exists")
	}
}

func TestNarrowParentsKeepTheirRollup(t *testing.T) {
	src := `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "a", "parent": "ui", "status": "done"}, {"id": "b", "parent": "ui"}]}`
	a, _, _ := openDemo(t, src, 80, 24)
	found := false
	for _, row := range a.frameRows().Rows {
		if strings.Contains(row, " ui ") && strings.Contains(row, "UI") {
			found = true
			if !strings.Contains(row, "1/2") {
				t.Fatalf("a parent shows its rollup at 38 columns: %q", row)
			}
		}
	}
	if !found {
		t.Fatalf("no parent row\n%s", a.frame())
	}
}

func TestPlanFilesThatAreNotFilesAreRefused(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero")
	}
	a := testApp(t)
	root := filepath.Join(a.plans.Repo, "rex", "plans")
	os.MkdirAll(root, 0o755)
	if os.Symlink("/dev/zero", filepath.Join(root, "zero.rex.json")) != nil {
		t.Skip("no symlinks")
	}
	done := make(chan struct{})
	go func() {
		b := NewPlanBrowser(a.plans.Repo)
		b.Rebuild()
		d := openDoc(filepath.Join(root, "zero.rex.json"), time.Now())
		if d.err == "" {
			t.Error("a device named .rex.json is not read as a plan")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading a device named .rex.json hung")
	}
}

func TestATornSaveOfTheSameBytesIsQuiet(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	os.WriteFile(path, []byte(""), 0o644)
	later(a, c)
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if strings.Contains(a.statusLine().Text, "plan updated") {
		t.Fatalf("toast %q", a.statusLine().Text)
	}
}

func TestOutlineComposerTakesVerbs(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	if frame := a.frame(); !strings.Contains(frame, "│ > ") {
		t.Fatalf("the outline's composer is the verb prompt\n%s", frame)
	}
	typeText(a, "zap")
	if frame := a.frame(); strings.Contains(frame, "⏎ detail") || !strings.Contains(frame, "⏎ not a verb") {
		t.Fatalf("the footer previews what ⏎ does with the draft\n%s", frame)
	}
}

func TestDependingOnAParentWaitsForItsLeaves(t *testing.T) {
	p, err := plan.Parse([]byte(`{"tasks": [
		{"id": "ui", "title": "UI"},
		{"id": "a", "parent": "ui", "status": "done"},
		{"id": "b", "parent": "ui", "status": "skipped"},
		{"id": "next", "depends_on": ["ui"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	g := p.Graph()
	if unmet := g.Unmet(p.Task("next")); len(unmet) != 0 {
		t.Fatalf("every leaf under ui is finished, so next can start: %v", unmet)
	}
	if pin := g.Pin(); pin == nil || pin.ID != "next" {
		t.Fatalf("pin %v", pin)
	}
}

func TestOutlineRuleKeepsProgressWhenNarrow(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "status": "done"}`, i))
	}
	tasks[39] = `{"id": "t40"}`
	src := `{"title": "A very long plan title that cannot fit", "status": "active", "tasks": [` + strings.Join(tasks, ",") + `]}`
	a, _, _ := openDemo(t, src, 80, 24)
	if frame := a.frame(); !strings.Contains(frame, "active · 39/40") {
		t.Fatalf("the narrow rule keeps the status and progress\n%s", frame)
	}
	press(a, "home")
	if frame := a.frame(); !strings.Contains(frame, "active · 39/40") || !strings.Contains(frame, "tasks 0/40") || !strings.Contains(frame, "✓ t01") {
		t.Fatalf("scrolled, it still does\n%s", frame)
	}
}

func TestNarrowestParentKeepsItsRollup(t *testing.T) {
	src := `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "a", "parent": "ui", "status": "done"}, {"id": "b", "parent": "ui"}]}`
	a, _, _ := openDemo(t, src, 24, 24) // the narrowest text column
	found := false
	for _, row := range a.frameRows().Rows {
		if strings.Contains(row, " ui ") {
			found = true
			if !strings.Contains(row, "1/2") {
				t.Fatalf("rollup lost at 24 cells: %q", row)
			}
		}
	}
	if !found {
		t.Fatalf("no parent row\n%s", a.frame())
	}
}

func TestATaskNeverWaitsOnItselfThroughItsParent(t *testing.T) {
	p, err := plan.Parse([]byte(`{"tasks": [
		{"id": "ui"},
		{"id": "a", "parent": "ui", "status": "done"},
		{"id": "card", "parent": "ui", "depends_on": ["ui"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	g := p.Graph()
	if unmet := g.Unmet(p.Task("card")); len(unmet) != 0 {
		t.Fatalf("card waits only on ui's other leaves, which are done: %v", unmet)
	}
}

func TestFilledPlanSelectsItsPin(t *testing.T) {
	a, c, path := openDemo(t, string(plan.Stub("demo")), 120, 32)
	if a.outline.sel != "" {
		t.Fatal("an empty plan has only the title")
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if a.outline.sel != "card-highlight" {
		t.Fatalf("when an agent fills the plan the cursor goes to the pin, got %q", a.outline.sel)
	}
}

func TestNarrowRuleKeepsProgress(t *testing.T) {
	a, _, _ := openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "A very long plan title that cannot fit"`, 1), 80, 24)
	rule := a.frameRows().Rows[2]
	if !strings.Contains(rule, "── A very") || !strings.Contains(rule, "· active · 1/3") {
		t.Fatalf("%q", rule)
	}
	// a tab is painted as spaces, so it is measured as spaces
	a, _, _ = openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Tabs\t\t\t\tin a long title"`, 1), 80, 24)
	if rule := a.frameRows().Rows[2]; !strings.Contains(rule, "── Tabs") || !strings.Contains(rule, "· active · 1/3") {
		t.Fatalf("%q", rule)
	}
}

func TestDetailShowsTheStackFields(t *testing.T) {
	p, err := plan.Parse([]byte(`{"base": "dev", "tasks": [{"id": "a", "branch": "feature/a"}, {"id": "b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &planDoc{path: "rex/plans/x.rex.json", plan: p}
	trim := regexp.MustCompile(` +\n`)
	if got := trim.ReplaceAllString(viewText(planDetail(d, 60)), "\n"); !strings.Contains(got, "stack base\n    dev\n") {
		t.Fatal(got)
	}
	if got := trim.ReplaceAllString(viewText(taskDetail(d, "a", 60)), "\n"); !strings.Contains(got, "branch\n    feature/a\n") {
		t.Fatal(got)
	}
	if got := viewText(taskDetail(d, "b", 60)); strings.Contains(got, "branch") {
		t.Fatal(got)
	}
}
