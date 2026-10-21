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
