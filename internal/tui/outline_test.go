package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	press(a, "esc") // from the wait screen to the plan list
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
		"REX [PLAN] AGENT idle",
		"latched rex/plans/cuj/demo.rex.json",
		"▸ card-highlight  Selected plan card is one block",
		"1/3 done",
		"  PLANS › Send-off demo",
		"active",
		"  Show a plan, pin a task, and send it off.",
		// The PINNED card: what a fresh chat needs, read from the plan
		// (docs/ux.md §6).
		"╭─[PINNED]─ card-highlight · Selected plan card is one block ─",
		"│ does    Bar, border, and fill cover the whole selected card",
		"│ needs   pick / pickbg colors exist",
		"│ verify  Open latest: ▌ on all three card rows",
		"│ files   internal/tui/plans.go",
		"│ done    ✓ plans-ui  Home cards, browse, duplicate warn",
		"╰───",
		"TASKS ─",
		"─ 1/3 done",
		"✓ plans-ui        Plans navigator · Home cards, browse, duplicate warn",
		"▌ ▸ card-highlight",
		"Selected plan card is one block · Bar, border, an…  pinned",
		"pinned",
		"· load-outline",
		"after card-highlight",
		"esc plans  tab agent  enter open  ? help  ↑↓ move",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	for _, never := range []string{"╭──", "/ find", "last ✓", "p⏎", "d⏎", "s⏎", "b⏎", "?⏎"} {
		if strings.Contains(frame, never) {
			t.Errorf("the outline has no composer and no verbs: %q\n%s", never, frame)
		}
	}
	if a.outline.sel != "card-highlight" {
		t.Fatalf("the pin is selected on open, got %q", a.outline.sel)
	}
}

// docs/ux.md §6: under 60 cells rows are title-only and the selected row
// gets its blurb on a row of its own, except the pin while the card shows
// what it does.
func TestOutlineNarrowShowsTheBlurbUnderTheSelection(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 80, 24)
	frame := a.frame()
	if strings.Contains(frame, "Plans navigator · ") {
		t.Fatal("rows are title-only below 60 columns")
	}
	if !strings.Contains(frame, "▌ ▸ card-highlight Selected") || strings.Contains(frame, "▌     Bar, border") || !strings.Contains(frame, "│ does    Bar, border") || !strings.Contains(frame, "▸ card-highlight  Selected plan…") {
		t.Fatalf("the card says what the pin does, so its row has no blurb row\n%s", frame)
	}
	press(a, "down")
	if frame := a.frame(); !strings.Contains(frame, "▌ · load-outline   Outline view") || !strings.Contains(frame, "▌     Show tasks") {
		t.Fatalf("another task's blurb is on its own row\n%s", frame)
	}
	press(a, "up", "up")
	if frame := a.frame(); !strings.Contains(frame, "▌ ✓ plans-ui") || !strings.Contains(frame, "▌     Home cards, browse, duplicate w…") {
		t.Fatalf("a done task's blurb is what it accomplished\n%s", frame)
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
		t.Fatal("enter on a task opens its detail")
	}
	frame := a.frame()
	for _, want := range []string{"PLANS › Send-off demo › card-highlight", "  [PINNED] active", "DOES ─", "VERIFY ─", "FILES ─", "CONTEXT ─", "  after   ✓ plans-ui  Home cards, browse, duplicate warn", "  then    · load-outline  Outline view", "  needs   pick / pickbg colors exist", "esc outline  tab agent  ↑↓ scroll"} {
		if !strings.Contains(frame, want) {
			t.Errorf("task detail missing %q\n%s", want, frame)
		}
	}
	press(a, "esc")
	if a.at != atOutline || a.outline.sel != "card-highlight" {
		t.Fatal("Esc goes back to the outline, selection kept")
	}
	press(a, "up", "up", "up")
	if a.outline.sel != "" || !strings.Contains(a.frame(), "▌ PLANS › Send-off demo") {
		t.Fatalf("the title row, the page title, is the top row, got %q\n%s", a.outline.sel, a.frame())
	}
	press(a, "\r")
	frame = a.frame()
	for _, want := range []string{"PLANS › Send-off demo › about", "  Send-off demo", "  active · 1 of 3 done  ━━━━━━━─────────────", "  rex/plans/cuj/demo.rex.json", "GOAL ─", "  Show a plan, pin a task", "SCOPE ─", "  in   • outline view", "  out  • extra palettes", "DONE ─", "  ✓ plans-ui  Home cards, browse, duplicate warn"} {
		if !strings.Contains(frame, want) {
			t.Errorf("plan detail missing %q\n%s", want, frame)
		}
	}
	press(a, "left")
	press(a, "down", "right")
	if a.at != atTaskDetail || a.outline.sel != "plans-ui" {
		t.Fatal("→ opens task detail")
	}
	if frame := a.frame(); !strings.Contains(frame, "  ✓ done") || !strings.Contains(frame, "DONE ─") || !strings.Contains(frame, "  did      Home cards, browse, duplicate warn") || !strings.Contains(frame, "  changed  internal/tui/plans.go") || strings.Index(frame, "DONE ─") > strings.Index(frame, "INTENDED ─") {
		t.Fatalf("a done task leads with what it did, then what it intended\n%s", frame)
	}
	press(a, "esc", "esc")
	if a.at != atLibrary || a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Group != groupOpen {
		t.Fatalf("Esc from the outline lands on OPEN: at=%d row=%+v", a.at, a.plans.Rows[a.plans.Cursor])
	}
	if !strings.Contains(a.frame(), "rex/plans/cuj/demo.rex.json") {
		t.Fatal("the plan stays open in the header")
	}
	press(a, "\r")
	if a.at != atOutline {
		t.Fatal("enter on the open plan reopens the outline")
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
	if a.statusLine().Text != "▸ card-highlight  Selected plan card is one block" {
		t.Fatalf("the toast expires: %q", a.statusLine().Text)
	}
	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatal("reading the plan never writes it")
	}
}

// The agent pins and finishes tasks in the file, and Rex follows: row 1,
// the outline and a toast (docs/ux.md §7, §8).
func TestTheAgentsPinAndDoneShow(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	done := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],
      "accomplished": ["the bar spans all three card rows"],
      "verified_how": ["go test ./internal/tui/"],
      "completed_notes": "the pick colors were already there",`, 1)
	os.WriteFile(path, []byte(done), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · next up load-outline" {
		t.Fatalf("the file finished the pin: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "card-highlight → done" {
		t.Fatalf("tape line %+v", last)
	}
	a.settle(80, 24) // 38 text cells: the done id clips, the next one stays, and progress yields
	if row := strings.TrimSpace(a.frameRows().Rows[1]); row != "✓ …ghlight done · next up load-outline" {
		t.Fatalf("row 1 at 80×24: %q", row)
	}
	a.settle(120, 32)
	c.t = c.t.Add(toastFor)
	a.tick()
	if got := a.statusLine().Text; got != "▸ load-outline  Outline view (next up)" {
		t.Fatalf("then the next up task, with no verb to type: %q", got)
	}
	// The card moves to what is next up, and names the task it builds on.
	if frame := a.frame(); !strings.Contains(frame, "╭─[NEXT UP]─ load-outline · Outline view") || !strings.Contains(frame, "│ done    ✓ card-highlight  the bar spans all three card rows") || !strings.Contains(frame, "✓ card-highlight  Selected plan card is one block · the bar spans") {
		t.Fatalf("the outline follows the file\n%s", frame)
	}
	press(a, "home", "down", "down", "\r")
	if a.at != atTaskDetail || a.outline.sel != "card-highlight" {
		t.Fatalf("at=%d sel=%q", a.at, a.outline.sel)
	}
	detail := viewText(taskDetail(a.doc, a.outline.sel, 78, true)) // it scrolls; all of it
	for _, want := range []string{"DONE ─", "  did      the bar spans all three card rows", "  checked  go test ./internal/tui/", "  notes    the pick colors were already there", "INTENDED ─", "  does     Bar, border, and fill cover the whole selected card"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("task detail shows %q\n%s", want, detail)
		}
	}
	active := strings.Replace(done, `"status": "pending"`, `"status": "active"`, 1)
	os.WriteFile(path, []byte(active), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "plan updated · load-outline → active" {
		t.Fatalf("the agent starts the next task: %q", got)
	}
	last := strings.Replace(active, `"status": "active",
      "depends_on": ["card-highlight"],`, `"status": "done",
      "depends_on": ["card-highlight"],`, 1)
	last = strings.Replace(last, `"status": "active",
  "scope"`, `"status": "done",
  "scope"`, 1)
	os.WriteFile(path, []byte(last), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ load-outline done · the plan is done" {
		t.Fatalf("the last leaf: %q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != last {
		t.Fatal("Rex never writes the plan")
	}
}

// docs/ux.md §8: when the file finishes the pin and what is left can't
// start, row 1 says so instead of naming a next task.
func TestTheAgentsDoneWithNothingUnblocked(t *testing.T) {
	src := strings.Replace(demoPlan, `"status": "pending",
      "depends_on": ["card-highlight"],`, `"status": "blocked",
      "depends_on": ["card-highlight"],`, 1)
	a, c, path := openDemo(t, src, 120, 32)
	done := strings.Replace(src, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`, 1)
	os.WriteFile(path, []byte(done), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · nothing else is unblocked" {
		t.Fatalf("the file finished the pin, the rest is blocked: %q", got)
	}
	if got, _ := os.ReadFile(path); string(got) != done {
		t.Fatal("Rex never writes the plan")
	}
}

// docs/ux.md §8: agents finish a task in several saves. One that only
// adds the retrospective keeps the done news on row 1; one that moves a
// task is news of its own.
func TestTheDoneToastOutlastsTheRetrospective(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	done := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`, 1)
	os.WriteFile(path, []byte(done), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · next up load-outline" {
		t.Fatalf("save 1: %q", got)
	}
	c.t = c.t.Add(time.Second)
	retro := strings.Replace(done, `"status": "done",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "accomplished": ["the bar spans all three card rows"],
      "depends_on": ["plans-ui"],`, 1)
	os.WriteFile(path, []byte(retro), 0o644)
	later(a, c) // 1.5 s after save 1
	if got := a.statusLine().Text; got != "✓ card-highlight done · next up load-outline" {
		t.Fatalf("save 2, the retrospective only: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "edited" {
		t.Fatalf("the tape still records it: %+v", last)
	}
	started := strings.Replace(retro, `"status": "pending"`, `"status": "active"`, 1)
	os.WriteFile(path, []byte(started), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "plan updated · load-outline → active" {
		t.Fatalf("a task move is news: %q", got)
	}
}

// When one save finishes the pin and starts the next task, the toast says
// what is on now, not what is next up.
func TestTheDoneToastSaysNowWhenTheNextIsStarted(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	both := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`, 1)
	both = strings.Replace(both, `"status": "pending"`, `"status": "active"`, 1)
	os.WriteFile(path, []byte(both), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · now load-outline" {
		t.Fatalf("row 1 %q", got)
	}
	a.settle(80, 24) // 38 text cells: the done id clips, what is on now stays, and progress yields
	if row := strings.TrimSpace(a.frameRows().Rows[1]); row != "✓ …d-highlight done · now load-outline" {
		t.Fatalf("38 cells: %q", row)
	}
}

// docs/ux.md §8: the plan is done when every leaf is done or skipped, as
// row 1's pin line judges it, even before the agent's next save sets the
// plan's own status; that save keeps the news and says so on the tape.
func TestThePlanIsDoneBeforeItsStatusSays(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	leaves := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],`, 1)
	leaves = strings.Replace(leaves, `"status": "pending"`, `"status": "skipped"`, 1)
	os.WriteFile(path, []byte(leaves), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · the plan is done" {
		t.Fatalf("every leaf is done or skipped, the plan's status not yet: %q", got)
	}
	whole := strings.Replace(leaves, `"status": "active",
  "scope"`, `"status": "done",
  "scope"`, 1)
	os.WriteFile(path, []byte(whole), 0o644)
	later(a, c)
	if got := a.statusLine().Text; got != "✓ card-highlight done · the plan is done" {
		t.Fatalf("the plan's own status keeps the done news: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "plan → done" {
		t.Fatalf("tape line %+v", last)
	}
	c.t = c.t.Add(toastFor)
	a.tick()
	if got := a.statusLine().Text; got != "no pin · all tasks done" {
		t.Fatalf("then row 1 agrees: %q", got)
	}
}

// The done toast keeps what is next when the row is narrow: the done id
// clips from the left, then the tail shortens.
func TestFitDone(t *testing.T) {
	for _, c := range []struct {
		width int
		want  string
	}{
		{44, "✓ card-highlight done · next up load-outline"},
		{38, "✓ …ghlight done · next up load-outline"},
		{34, "✓ …hlight done · next load-outline"},
		{20, "✓ done · next load-outline"}, // clip cuts the rest
	} {
		got := fitDone("card-highlight", " · next up load-outline", " · next load-outline", c.width)
		if got != c.want || (c.width > 20 && cellLen(got) > c.width) {
			t.Errorf("width %d: %q, want %q", c.width, got, c.want)
		}
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
	if !strings.Contains(a.frame(), "▸ card-highlight") {
		t.Fatal("the last good plan stays on screen")
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

// docs/ux.md §6, Plan states: what stands in for the PINNED card, and the
// red lines under the goal.
func TestOutlineStates(t *testing.T) {
	// headOf is what stays put over the tasks: the warnings are red.
	head := func(a *app) []LogLine {
		view := a.composeOptions().PlanView
		return view[:headOf(view)]
	}
	a, _, _ := openDemo(t, `{"version": 2, "goal": "the goal", "tasks": [{"id": "a", "title": "A"}]}`, 120, 32)
	frame := a.frame()
	if !strings.Contains(frame, "read-only: version 2 · newer than this Rex reads") || strings.Contains(frame, "to edit") || !strings.Contains(frame, "esc plans  tab agent  enter open") {
		t.Fatalf("%s", frame)
	}
	if h := head(a); strings.TrimRight(h[0].Text, " ") != "  the goal" || h[1].Kind != "error" || !strings.HasPrefix(h[1].Text, "  read-only: ") {
		t.Fatalf("the red line goes under the goal: %+v", h)
	}
	b, _, _ := openDemo(t, `{"tasks": [{"id": "a", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]}]}`, 120, 32)
	frame = b.frame()
	for _, want := range []string{"after ?zz", "cycle", "! 2 problems · enter on the title lists them", "no pin · nothing unblocked", "TASKS ─"} {
		if !strings.Contains(frame, want) {
			t.Errorf("missing %q\n%s", want, frame)
		}
	}
	if body := planBody(b); strings.Contains(body, "╭─[") || !strings.Contains("\n"+body, "\n  no pin · nothing unblocked") {
		t.Errorf("no pin, no card: a quiet line stands in for it\n%s", body)
	}
	c, _, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "skipped"}]}`, 120, 32)
	if frame := c.frame(); c.statusLine().Text != "no pin · all tasks done" || !strings.Contains(frame, "  ✓ all tasks done") || !strings.Contains(frame, "– b") || !strings.Contains(frame, "skipped") {
		t.Fatalf("%q\n%s", c.statusLine().Text, frame)
	}
	d, _, _ := openDemo(t, `{"tasks": [`, 120, 32)
	if frame := d.frame(); !strings.Contains(frame, "can't read this plan") || !strings.Contains(frame, "PLANS › demo") || !strings.Contains(frame, "can't read ") || !strings.HasPrefix(d.statusLine().Text, "plan JSON error 1:") || strings.Contains(frame, "╭─[") {
		t.Fatalf("%q\n%s", d.statusLine().Text, frame)
	}
	// Two active: the card shows the first, the second keeps ▸ and says so.
	e, _, _ := openDemo(t, `{"tasks": [{"id": "a", "title": "A", "status": "active"}, {"id": "b", "title": "B", "status": "active"}]}`, 80, 24)
	if frame := e.frame(); e.statusLine().Text != "2 active: a, b · pinned a" || !strings.Contains(frame, "╭─[PINNED]─ a ─") || !strings.Contains(frame, "  ▸ b B") || !strings.Contains(frame, "also active") {
		t.Fatalf("%q\n%s", e.statusLine().Text, frame)
	}
	// A red tag shows at every width (docs/ux.md §6, Rows).
	e.settle(24, 24)
	if body := planBody(e); !strings.Contains("\n"+body+"\n", "\n  ▸ b B      also active\n") {
		t.Errorf("24 cells keep also active\n%s", body)
	}
	// An active task with no id is never the pin, and the pin isn't
	// active: "also active" would point at nothing.
	n, _, _ := openDemo(t, `{"tasks": [{"title": "noid", "status": "active"}, {"id": "b", "title": "B"}]}`, 120, 24)
	if body := planBody(n); n.statusLine().Text != "▸ b  B (next up)" || !strings.Contains(body, "[NEXT UP]─ b") || !strings.Contains("\n"+body, "\n  ▸ #0  noid\n") || strings.Contains(body, "also active") {
		t.Errorf("%q\n%s", n.statusLine().Text, body)
	}
	press(n, "home")
	if press(n, "down"); n.outline.sel != "#0" {
		t.Fatalf("sel %q", n.outline.sel)
	}
	if press(n, "\r"); !strings.Contains(planBody(n), "\n  ▸ active\n") {
		t.Errorf("task detail says active, not also active\n%s", planBody(n))
	}
	// The warnings wrap to two rows each and are the ladder's last step:
	// at 80×12 the goal and TASKS are gone, the strip stays under them.
	w, _, _ := openDemo(t, `{"version": 2, "title": "Warned", "goal": "a goal", "tasks": [{"id": "a", "status": "active", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]}, {"id": "d"}, {"id": "e"}, {"id": "f"}]}`, 80, 12)
	if body := planBody(w); !strings.HasPrefix(body, "  read-only: version 2 · newer than\n    this Rex reads\n  ! 3 problems · enter on the title\n    lists them\n[PINNED] a\n▌ ▸ a ") || strings.Contains(body, "a goal") || strings.Contains(body, "TASKS") {
		t.Errorf("80×12: the warnings, two rows each, over the strip\n%s", body)
	}
	if w.settle(80, 8); strings.Contains(planBody(w), "read-only") || !strings.HasPrefix(planBody(w), "[PINNED] a\n▌ ▸ a ") {
		t.Errorf("80×8: the warnings go last, for the task rows, and the strip fits again\n%s", planBody(w))
	}
	f, _, _ := openDemo(t, `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "x", "parent": "ui", "status": "done"}, {"id": "y", "parent": "ui"}]}`, 120, 32)
	if frame := f.frame(); !strings.Contains(frame, "1/2") || f.outline.sel != "y" || !strings.Contains(frame, "[NEXT UP]─ y") {
		t.Fatalf("a parent shows a rollup and is never the pin (sel %q)\n%s", f.outline.sel, frame)
	}
	// An empty plan: one line, no TASKS, the title row selected.
	g, _, _ := openDemo(t, string(plan.Stub("demo")), 80, 24)
	if frame := planBody(g); !strings.HasPrefix(frame, "  No tasks yet · your agent fills this\n  file\n") || strings.Contains(frame, "TASKS") || g.outline.sel != "" {
		t.Fatalf("%s", frame)
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
	if a.outline.sel != "t12" || !strings.Contains(frame, "─ 12 of 40") || !strings.Contains(frame, "↓ more") || !strings.Contains(frame, "▌ ○ t12") {
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

// The outline has no composer: letters, Backspace and a paste do nothing,
// ? opens Help at once, and Esc goes straight to the plan list (docs/ux.md
// §2, §3).
func TestOutlineKeysActAtOnce(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	before, _ := os.ReadFile(path)
	frame := a.frame()
	typeText(a, "zap pdsb")
	press(a, "\x7f", "\x15", "\x17", PastePrefix+"pin load-outline")
	if a.frame() != frame || a.draft != "" {
		t.Fatalf("draft %q\n%s", a.draft, a.frame())
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("nothing typed on the outline writes the plan")
	}
	if press(a, "?"); a.overlay[PanePlans] != overlayHelp {
		t.Fatal("? opens Help at once")
	}
	if press(a, "esc", "esc"); a.at != atLibrary {
		t.Fatalf("Esc closes Help, then goes up to the plan list: at=%d", a.at)
	}
}

// Start reopens the plan last opened in this repo, with no keys, and
// follows it (docs/ux.md §11, §14); Esc shows it under OPEN.
func TestOpenGroupAfterRestart(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	// A newer plan doesn't win over the one in ~/.rex/state.json.
	other := filepath.Join(a.plans.Root, "zz-newer.rex.json")
	os.WriteFile(other, []byte(demoPlan), 0o644)
	again := newApp("yellow", false, "idle", NewPlanBrowser(a.plans.Repo), 120, 32)
	if again.at != atOutline || again.plans.Current != path || !again.follow || again.outline.sel != "card-highlight" {
		t.Fatalf("at=%d current=%q follow=%v sel=%q", again.at, again.plans.Current, again.follow, again.outline.sel)
	}
	if frame := again.frame(); !strings.Contains(frame, "following rex/plans/cuj/demo.rex.json") {
		t.Fatalf("the header names the plan and that Rex follows\n%s", frame)
	}
	press(again, "esc")
	row := again.plans.Rows[again.plans.Cursor]
	if row.Group != groupOpen || row.Path != path {
		t.Fatalf("cursor on %+v", row)
	}
	if frame := again.frame(); !strings.Contains(frame, "OPEN ─") || !strings.Contains(frame, "▌ cuj/demo ") || !strings.Contains(frame, "▌   pinned  ▸ card-highlight") {
		t.Fatalf("%s", frame)
	}
}

// The page title and the TASKS H2 stay as the list scrolls; TASKS says
// where the selection is in it, and the page title keeps the plan's status
// alone (docs/ux.md §6).
func TestOutlinePageTitleStaysAsTheListScrolls(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d"}`, i, i))
	}
	a, _, _ := openDemo(t, `{"title": "Big", "tasks": [`+strings.Join(tasks, ",")+`]}`, 120, 24)
	for i := 0; i < 30; i++ {
		press(a, "down")
	}
	frame := a.frame()
	if !strings.Contains(frame, "PLANS › Big") || !strings.Contains(frame, "TASKS ─") || !strings.Contains(frame, "─ 31 of 40") || !strings.Contains(frame, "↑ more") || !strings.Contains(frame, "▌ ○ t31") || !strings.Contains(frame, "[NEXT UP]─ t01") {
		t.Fatalf("deep in the list, the page title, the card and the position stay on screen\n%s", frame)
	}
	b, _, _ := openDemo(t, `{"title": "Rex visual redesign", "status": "active", "tasks": [`+strings.Join(tasks, ",")+`]}`, 80, 24)
	press(b, "down", "down", "down")
	for _, cols := range []int{80, 120} {
		b.settle(cols, 24)
		title := textOf(b.frameRows().Rows[2], cols)
		if !strings.HasPrefix(title, "  PLANS › Rex visual redesign ") || !strings.HasSuffix(title, " active") || !strings.Contains(b.frame(), "─ 4 of 40") {
			t.Fatalf("%d columns: page title %q\n%s", cols, title, b.frame())
		}
	}
	press(b, "home")
	if frame := b.frame(); !strings.Contains(frame, "─ 0/40 done") || strings.Contains(frame, " of 40") {
		t.Fatalf("on the title row TASKS shows the progress\n%s", frame)
	}
	c, _, _ := cujApp(t)
	c.settle(80, 7)
	if title := textOf(c.frameRows().Rows[2], 80); !strings.HasPrefix(title, "  PLANS › Send-off demo") || !strings.HasSuffix(title, " active") {
		t.Fatalf("the CUJ outline at 80×7 scrolls and keeps its status: %q", title)
	}
}

func TestReopeningKeepsTheOpenPlan(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down") // load-outline
	sel := a.outline.sel
	press(a, "esc") // the plan list, the open plan selected
	os.WriteFile(path, []byte(`{"tasks": [`), 0o644)
	c.t = c.t.Add(docPollEvery + time.Millisecond)
	press(a, "\r")
	if a.at != atOutline || a.outline.sel != sel {
		t.Fatalf("reopening keeps the selection: at=%v sel=%q, want %q", a.at, a.outline.sel, sel)
	}
	if a.doc.plan == nil || a.doc.err != "" {
		t.Fatalf("a half-saved file right after reopening keeps the last good copy: err=%q", a.doc.err)
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

// The outline's body takes the rows a composer would have used.
func TestOutlineHasNoComposer(t *testing.T) {
	for _, rows := range []int{7, 12, 24} {
		a, _, _ := openDemo(t, demoPlan, 120, rows)
		if f := a.frame(); strings.Contains(f, "│ > ") || strings.Contains(f, " > ") {
			t.Fatalf("%d rows: no composer\n%s", rows, f)
		}
		if got, want := a.layout().bodyH, rows-4; got != want {
			t.Fatalf("%d rows: body %d, want %d", rows, got, want)
		}
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

// Narrow, progress stays on row 1 whatever the title; the page title's
// root gives way to the title, which clips, and the title row stays
// selectable (docs/ux.md §4).
func TestNarrowOutlineKeepsProgressOnRowOne(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "status": "done"}`, i))
	}
	tasks[39] = `{"id": "t40"}`
	src := `{"title": "A very long plan title that cannot fit", "status": "active", "tasks": [` + strings.Join(tasks, ",") + `]}`
	a, _, _ := openDemo(t, src, 80, 24)
	rows := a.frameRows().Rows
	if !strings.HasSuffix(strings.TrimRight(rows[1], " "), "39/40") || !strings.Contains(rows[2], "  … › A very long plan title that can…") {
		t.Fatalf("row 1 keeps the progress, row 2 the title\n%s", a.frame())
	}
	press(a, "home")
	if frame := a.frame(); !strings.Contains(frame, "39/40") || !strings.Contains(frame, "▌ … › A very") || !strings.Contains(frame, "✓ t01") {
		t.Fatalf("scrolled to the top, it still does\n%s", frame)
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

func TestNarrowPageTitleClipsTheTitle(t *testing.T) {
	a, _, _ := openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "A very long plan title that cannot fit"`, 1), 80, 24)
	title := a.frameRows().Rows[2]
	if !strings.Contains(title, "  … › A very long plan title that can…") || cellLen(title) != 80 || !strings.Contains(a.frameRows().Rows[1], "Selected plan…") {
		t.Fatalf("%q", title)
	}
	// a tab is painted as spaces, so it is measured as spaces
	a, _, _ = openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Tabs\t\t\t\tin a long title"`, 1), 80, 24)
	if title := a.frameRows().Rows[2]; !strings.Contains(title, "  … › Tabs ") || cellLen(title) != 80 || strings.Contains(title, "\t") {
		t.Fatalf("%q", title)
	}
}

func TestDetailShowsTheStackFields(t *testing.T) {
	p, err := plan.Parse([]byte(`{"base": "dev", "tasks": [{"id": "a", "branch": "feature/a"}, {"id": "b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &planDoc{path: "rex/plans/x.rex.json", plan: p}
	trim := regexp.MustCompile(` +\n`)
	if got := trim.ReplaceAllString(viewText(planDetail(d, "rex/plans/x.rex.json", 60, false)), "\n"); !strings.Contains(got, "STACK ─") || !strings.Contains(got, "\n  base    dev\n") {
		t.Fatal(got)
	}
	if got := trim.ReplaceAllString(viewText(taskDetail(d, "a", 60, false)), "\n"); !strings.Contains(got, "CONTEXT ─") || !strings.Contains(got, "\n  branch  feature/a\n") {
		t.Fatal(got)
	}
	if got := viewText(taskDetail(d, "b", 60, false)); strings.Contains(got, "branch") {
		t.Fatal(got)
	}
}

// redesignPlan has parents and every kind of leaf (docs/ux.md §6): done
// with its retrospective, the active pin with every card field, waiting,
// blocked and skipped.
const redesignPlan = `{
  "version": 1,
  "id": "redesign",
  "title": "Rex visual redesign",
  "goal": "A newcomer always knows which tab and page they are on, what is pinned, and which key gets them where.",
  "status": "active",
  "base": "dev",
  "scope": {"in": ["chrome", "outline"], "out": ["extra palettes"]},
  "tasks": [
    {"id": "chrome", "title": "Chrome: tabs, page titles, keys"},
    {"id": "tab-bar", "title": "Tab bar on row 0", "parent": "chrome", "status": "done", "does": ["Both tabs on row 0"],
     "accomplished": ["Two tabs on row 0, the active one filled"], "modified": ["internal/tui/chrome.go"],
     "verified_how": ["go test ./internal/tui -run TabBar"], "completed_notes": "the cue cell is fixed width"},
    {"id": "page-titles", "title": "Page titles and footer keys", "parent": "chrome", "status": "active", "depends_on": ["tab-bar"],
     "needs": ["the tab bar on row 0"], "does": ["Every screen opens with a title row that says where you are and where esc goes"],
     "verify": ["go test ./internal/tui -run PageTitle", "tmux at 80×24: every screen names itself"],
     "files": ["internal/tui/chrome.go", "internal/tui/outline.go", "internal/tui/plans.go"], "branch": "feature/page_titles"},
    {"id": "agent-stream", "title": "Fold runs of thoughts", "depends_on": ["page-titles"], "does": ["One row per settled run of thinking"]},
    {"id": "plan-views", "title": "Plan list and outline"},
    {"id": "plan-list", "title": "Plan list groups and accordion", "parent": "plan-views", "depends_on": ["page-titles"], "does": ["OPEN, RECENT and FOLDERS"]},
    {"id": "pin-card", "title": "Pinned card on the outline", "parent": "plan-views", "status": "blocked", "depends_on": ["plan-list"]},
    {"id": "width-table", "title": "Glyph widths in width_test", "status": "skipped"}
  ]
}`

// planBody is a frame's text column under the page title and over the footer.
func planBody(a *app) string {
	rows := a.frameRows().Rows
	var out []string
	for _, r := range rows[3 : len(rows)-1] {
		out = append(out, textOf(r, a.columns))
	}
	return strings.Join(out, "\n")
}

// docs/ux.md §6: the collapse ladder's card tiers on the CUJ plan, as
// docs/ux.md §6's mockups draw them: the whole card at 80×24, then its
// done row goes, then the second rows and the blank rows (needs comes
// back once they are gone), then the compact card, the strip, and at 7
// rows only the page title and three tasks. What fits again after a bigger
// step comes back.
func TestOutlineCardTiers(t *testing.T) {
	for _, tc := range []struct {
		cols, rows int
		want       string
	}{
		{80, 24, `  A teammate opens this plan in Rex a…

╭─[PINNED]─ card-highlight ──────────╮
│ Selected plan card is one block    │
│ does    Bar, border and fill cover │
│         all three rows of the sel… │
│ needs   the pick and pickbg color… │
│ verify  go test ./internal/tui -r… │
│         Open latest: ▌ on all thr… │
│ files   plans.go  live.go  +1      │
│ done    ✓ plans-ui  Home cards, f… │
╰────────────────────────────────────╯

TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view


`},
		{80, 20, `  A teammate opens this plan in Rex a…

╭─[PINNED]─ card-highlight ──────────╮
│ Selected plan card is one block    │
│ does    Bar, border and fill cover │
│         all three rows of the sel… │
│ needs   the pick and pickbg color… │
│ verify  go test ./internal/tui -r… │
│         Open latest: ▌ on all thr… │
│ files   plans.go  live.go  +1      │
╰────────────────────────────────────╯

TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		// The second rows and the blank rows make room for needs again;
		// verify says what its one row leaves out.
		{80, 16, `  A teammate opens this plan in Rex a…
╭─[PINNED]─ card-highlight ──────────╮
│ Selected plan card is one block    │
│ does    Bar, border and fill cove… │
│ needs   the pick and pickbg color… │
│ verify  go test ./internal/tu…  +1 │
│ files   plans.go  live.go  +1      │
╰────────────────────────────────────╯
TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		{80, 12, `╭─[PINNED]─ card-highlight ──────────╮
│ Selected plan card is one block    │
│ does    Bar, border and fill cove… │
╰────────────────────────────────────╯
TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		// The strip leaves the card's does to the pin's own blurb row, and
		// makes room for the goal again.
		{80, 11, `  A teammate opens this plan in Rex a…
[PINNED] card-highlight  Selected pla…
TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
▌     Bar, border and fill cover all…
  · load-outline   Outline view`},
		// TASKS goes before the strip (docs/ux.md §6, ladder steps 8 and 9).
		{80, 8, `[PINNED] card-highlight  Selected pla…
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		{80, 7, `  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		{120, 24, `  A teammate opens this plan in Rex and sees what the work is and which task …

╭─[PINNED]─ card-highlight · Selected plan card is one block ────────────────╮
│ does    Bar, border and fill cover all three rows of the selected card     │
│ needs   the pick and pickbg colors exist in mascot.UI                      │
│ verify  go test ./internal/tui -run PlanCardSelection                      │
│         Open latest: ▌ on all three card rows                              │
│ files   plans.go  live.go  plans_test.go                                   │
│ done    ✓ plans-ui  Home cards, folder browser, duplicate names refused    │
╰────────────────────────────────────────────────────────────────────────────╯

TASKS ─────────────────────────────────────────────────────────────── 1/3 done
  ✓ plans-ui        Plans navigator · Home cards, folder browser, duplicate n…
▌ ▸ card-highlight  Selected plan card is one block · Bar, border and…  pinned
  · load-outline    Outline view · Opening a plan shows…  after card-highlight




`},
		// From 60 cells the title is in the card's edge: the compact card is
		// 3 rows, and the goal fits over it.
		{120, 12, `  A teammate opens this plan in Rex and sees what the work is and which task …
╭─[PINNED]─ card-highlight · Selected plan card is one block ────────────────╮
│ does    Bar, border and fill cover all three rows of the selected card     │
╰────────────────────────────────────────────────────────────────────────────╯
TASKS ─────────────────────────────────────────────────────────────── 1/3 done
  ✓ plans-ui        Plans navigator · Home cards, folder browser, duplicate n…
▌ ▸ card-highlight  Selected plan card is one block · Bar, border and…  pinned
  · load-outline    Outline view · Opening a plan shows…  after card-highlight`},
		// Under 30 cells the card is always the strip.
		{24, 12, `  A teammate opens this…

[PINNED] card-highlight

TASKS ────────────── 1/3
  ✓ plans-ui Plans navi…
▌ ▸ card-hi… Selected p…
  · load-ou… Outline vi…`},
		{24, 7, `  ✓ plans-ui Plans navi…
▌ ▸ card-hi… Selected p…
  · load-ou… Outline vi…`},
	} {
		a, _, _ := cujApp(t)
		a.settle(tc.cols, tc.rows)
		if got := planBody(a); got != tc.want {
			t.Errorf("%d×%d:\n%s\nwant\n%s", tc.cols, tc.rows, got, tc.want)
		}
	}
	// A pin with nothing but a title, an agent's first save, says so inside
	// its box, never an empty one; the compact card's one row is the first
	// field the pin has.
	bare := `{"version": 1, "title": "Fresh", "goal": "g", "tasks": [{"id": "a", "title": "First thing", "status": "active"}, {"id": "b", "title": "Second thing", "depends_on": ["a"]}]}`
	untitled := `{"version": 1, "tasks": [{"id": "a", "status": "active"}, {"id": "b", "title": "B"}]}`
	verifyOnly := strings.NewReplacer(`"does": ["Bar, border and fill cover all three rows of the selected card"],`, "", `"needs": ["the pick and pickbg colors exist in mascot.UI"],`, "").Replace(string(cujFixture(t)))
	for _, tc := range []struct {
		src        string
		cols, rows int
		want       string
	}{
		{bare, 120, 13, `  g

╭─[PINNED]─ a · First thing ─────────────────────────────────────────────────╮
│ no does, verify or files yet · the agent adds them                         │
╰────────────────────────────────────────────────────────────────────────────╯

TASKS ─────────────────────────────────────────────────────────────── 0/2 done
▌ ▸ a  First thing                                                      pinned
  · b  Second thing                                                    after a`},
		{bare, 80, 12, `  g
╭─[PINNED]─ a ───────────────────────╮
│ First thing                        │
│ no does, verify or files yet       │
╰────────────────────────────────────╯
TASKS ─────────────────────── 0/2 done
▌ ▸ a First thing
  · b Second thing`},
		{untitled, 80, 12, `╭─[PINNED]─ a ───────────────────────╮
│ no does, verify or files yet       │
╰────────────────────────────────────╯

TASKS ─────────────────────── 0/2 done
▌ ▸ a (untitled)
  ○ b B
`},
		{verifyOnly, 80, 12, `╭─[PINNED]─ card-highlight ──────────╮
│ Selected plan card is one block    │
│ verify  go test ./internal/tu…  +1 │
╰────────────────────────────────────╯
TASKS ─────────────────────── 1/3 done
  ✓ plans-ui       Plans navigator
▌ ▸ card-highlight Selected plan card…
  · load-outline   Outline view`},
		{verifyOnly, 120, 11, `╭─[PINNED]─ card-highlight · Selected plan card is one block ────────────────╮
│ verify  go test ./internal/tui -run PlanCardSelection  +1                  │
╰────────────────────────────────────────────────────────────────────────────╯
TASKS ─────────────────────────────────────────────────────────────── 1/3 done
  ✓ plans-ui        Plans navigator · Home cards, folder browser, duplicate n…
▌ ▸ card-highlight  Selected plan card is one block                     pinned
  · load-outline    Outline view · Opening a plan shows…  after card-highlight`},
	} {
		a, _, _ := openDemo(t, tc.src, tc.cols, tc.rows)
		if got := planBody(a); got != tc.want {
			t.Errorf("%d×%d:\n%s\nwant\n%s", tc.cols, tc.rows, got, tc.want)
		}
	}
	// 30 text cells leave the box 26 inside: the hint shrinks, never cut.
	if n, _, _ := openDemo(t, untitled, 72, 12); !strings.Contains(planBody(n), "│ no details yet ") || strings.Contains(planBody(n), "y…") {
		t.Errorf("72×12:\n%s", planBody(n))
	}
}

// docs/ux.md §6: whatever the size, the ladder drops head pieces before
// task rows: at least 3 task rows are in view (5 from 16 rows, or every
// one of a shorter list), wherever the cursor is, and ↑ more and ↓ more
// never take one of them, but on the title row, where ↓ more takes the
// window's last row.
func TestOutlineKeepsTasks(t *testing.T) {
	var many []string
	for i := 1; i <= 40; i++ {
		many = append(many, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d", "does": ["step %d"]}`, i, i, i))
	}
	plans := map[string]string{
		"cuj":      string(cujFixture(t)),
		"redesign": redesignPlan,
		"many":     `{"title": "Big", "goal": "a long list", "tasks": [` + strings.Join(many, ",") + `]}`,
		// read-only and problems: the ladder's last step drops the warnings.
		"warned": `{"version": 2, "title": "Warned", "goal": "a goal", "tasks": [{"id": "a", "status": "active", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]},` + strings.Join(many[:6], ",") + `]}`,
	}
	for name, src := range plans {
		for _, cols := range []int{24, 80, 120} {
			a, _, _ := openDemo(t, src, cols, 24)
			p := a.doc.plan
			n := len(outlineRows(p)) - 1
			for lines := 7; lines <= 30; lines++ {
				a.settle(cols, lines)
				for _, sel := range []string{defaultSel(p), "", outlineRows(p)[1], outlineRows(p)[n/2+1], outlineRows(p)[n]} {
					a.outline.sel = sel
					opt := a.composeOptions()
					view, bodyH := opt.PlanView, a.layout().bodyH
					head := headOf(view)
					var tasks []LogLine
					for _, l := range view[head:] {
						if l.Kind != "idle" { // ↑ more, ↓ more
							tasks = append(tasks, l)
						}
					}
					want := min(n, 3)
					if lines >= 16 {
						want = min(n, 5)
					}
					if sel == "" && strings.Contains(viewText(view), "↓ more") {
						want-- // the title row is selected: ↓ more takes the window's last row
					}
					if got := taskRows(tasks); got < want || len(view) > bodyH {
						t.Errorf("%s %d×%d on %s: %d task rows in view, want %d (%d lines in %d rows)\n%s", name, cols, lines, sel, got, want, len(view), bodyH, a.frame())
					}
					if sel != "" && !slices.ContainsFunc(tasks, func(l LogLine) bool { return l.Selected }) {
						t.Errorf("%s %d×%d: the selected row %s is dropped\n%s", name, cols, lines, sel, a.frame())
					}
					if f := a.frameRows(); len(f.Rows) != lines {
						t.Errorf("%s %d×%d: %d rows", name, cols, lines, len(f.Rows))
					}
				}
			}
		}
	}
}

// docs/ux.md §6: a blank row goes between top-level groups (a parent and
// the tasks under it) only when the whole list fits; never inside a group.
func TestOutlineGroupGaps(t *testing.T) {
	gapBefore := func(body, row string) bool {
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, row) {
				return i > 0 && strings.TrimSpace(lines[i-1]) == ""
			}
		}
		t.Fatalf("no row %q\n%s", row, body)
		return false
	}
	for _, size := range [][2]int{{120, 40}, {24, 24}} {
		a, _, _ := openDemo(t, redesignPlan, size[0], size[1])
		body := planBody(a)
		for _, row := range []string{"  · agent-s", "    plan-vi", "  – width-t"} {
			if !gapBefore(body, row) {
				t.Errorf("%dx%d: no blank row before %q\n%s", size[0], size[1], row, body)
			}
		}
		for _, row := range []string{"    ✓ tab-bar", "▌   ▸ page-ti", "    · plan-li", "    ! pin-card"} {
			if gapBefore(body, row) {
				t.Errorf("%dx%d: a blank row inside a group, before %q\n%s", size[0], size[1], row, body)
			}
		}
	}
	// The list scrolls at 80×24: no blank rows in it.
	a, _, _ := openDemo(t, redesignPlan, 80, 24)
	body := strings.TrimRight(planBody(a), "\n ")
	list := body[strings.Index(body, "TASKS ─"):]
	if !strings.Contains(list, "3 of 8") || strings.Contains(list, "\n\n") {
		t.Errorf("a list that scrolls has no blank rows\n%s", body)
	}
}

// docs/ux.md §5, §6: the pin's ▸ and id are the only yellow on the list,
// in bold, and keep their yellow on the selection's fill; parents are bold
// text; "pinned" tags the pin's row only from 60 cells.
func TestPinRowStyle(t *testing.T) {
	for _, cols := range []int{80, 120} {
		a, _, _ := openDemo(t, redesignPlan, cols, 32)
		for _, sel := range []string{"page-titles", "agent-stream"} {
			a.outline.sel = sel
			view := a.composeOptions().PlanView
			pins := 0
			for _, l := range view[headOf(view):] {
				role := func(s Span) string {
					if s.Role != "" {
						return s.Role
					}
					return l.Kind
				}
				name := strings.Fields(strings.TrimLeft(l.Text, "▌ "))
				switch {
				case len(name) > 1 && name[1] == "page-titles":
					pins++
					if len(l.Spans) < 3 || l.Spans[2].Text != "▸ page-titles" || l.Spans[2].Role != "title" || !l.Spans[2].Bold {
						t.Errorf("%d cols: the pin's ▸ and id are bold yellow: %+v", cols, l.Spans)
					}
					if (sel == "page-titles") != (l.Fill == "pickbg") {
						t.Errorf("%d cols: the selection's fill: %+v", cols, l)
					}
					if tagged := strings.HasSuffix(strings.TrimRight(l.Text, " "), "pinned"); tagged != (cols == 120) {
						t.Errorf("%d cols: pinned tag %v: %q", cols, tagged, l.Text)
					}
					continue
				case len(name) > 0 && (name[0] == "chrome" || name[0] == "plan-views"):
					if l.Kind != "text" || !l.Spans[2].Bold || role(l.Spans[2]) != "text" || !l.Spans[4].Bold {
						t.Errorf("%d cols: a parent is bold text: %+v", cols, l)
					}
				}
				if l.Kind == "title" || l.Kind == "accent" {
					t.Errorf("%d cols: only the pin is yellow: %+v", cols, l)
				}
				for _, s := range l.Spans {
					if role(s) == "title" || role(s) == "accent" {
						t.Errorf("%d cols: only the pin is yellow: %q in %q", cols, s.Text, l.Text)
					}
				}
			}
			if pins != 1 {
				t.Errorf("%d cols: %d pin rows", cols, pins)
			}
		}
	}
}

// docs/ux.md §6, §7: with no task active the pin is worked out: the card
// says [NEXT UP] on a slate pill, row 1 says (next up), and the pin's row
// is tagged next up; the agent's active task is [PINNED] on yellow.
func TestNextUpBadge(t *testing.T) {
	planned := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "pending",
      "depends_on": ["plans-ui"],`, 1)
	a, c, path := openDemo(t, planned, 120, 32)
	frame := a.frame()
	if !strings.Contains(frame, "╭─[NEXT UP]─ card-highlight · Selected plan card is one block ─") || !strings.Contains(frame, "next up") || a.statusLine().Text != "▸ card-highlight  Selected plan card is one block (next up)" {
		t.Fatalf("%q\n%s", a.statusLine().Text, frame)
	}
	badge := func() []Span {
		for _, l := range a.composeOptions().PlanView {
			if strings.HasPrefix(l.Text, "╭─[") {
				return l.Spans[1:4]
			}
		}
		t.Fatal("no card")
		return nil
	}
	if b := badge(); b[1].Text != "NEXT UP" || b[1].Role != "title" || b[1].Fill != "border" || b[0].Fill != "border" || b[0].Role != "border" || !b[1].Bold {
		t.Fatalf("[NEXT UP] is yellow on slate: %+v", b)
	}
	// Row 1's "(next up)" would satisfy a frame-wide look, so the row's
	// own tag is checked on the row: yellow from 60 cells, none under.
	pinRow := func(a *app) (LogLine, bool) {
		view := a.composeOptions().PlanView
		for _, l := range view[headOf(view):] {
			if strings.HasPrefix(strings.TrimLeft(l.Text, "▌ "), "▸ card-highlight") {
				return l, true
			}
		}
		return LogLine{}, false
	}
	if l, ok := pinRow(a); !ok || len(l.Spans) == 0 || l.Spans[len(l.Spans)-1].Text != "next up" || l.Spans[len(l.Spans)-1].Role != "title" {
		t.Fatalf("the pin's row is tagged next up in yellow: %+v\n%s", l, frame)
	}
	narrow, _, _ := openDemo(t, planned, 80, 32)
	if l, ok := pinRow(narrow); !ok || strings.HasSuffix(strings.TrimRight(l.Text, " "), "next up") {
		t.Fatalf("80 cols: the pin's row has no tag under 60 cells: %+v\n%s", l, narrow.frame())
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if b := badge(); b[1].Text != "PINNED" || b[1].Role != "pickbg" || b[1].Fill != "title" || b[0].Role != "title" || !strings.Contains(a.frame(), "╭─[PINNED]─ card-highlight") {
		t.Fatalf("[PINNED] is ink on yellow: %+v\n%s", b, a.frame())
	}
	if a.enabled = true; !strings.Contains(stripSGR(a.frame()), "[PINNED]") {
		t.Fatal("color never changes the characters: the brackets are painted, not dropped")
	}
}

// docs/ux.md §6: task detail runs DOES, VERIFY, FILES and CONTEXT under
// the pin's status line, and SO FAR for what a task not done yet says it
// did; a done task leads with DONE, then INTENDED; About has GOAL, SCOPE,
// STACK and DONE, the send-off's Done kept in the plan, its count saying
// how many finished tasks were skipped.
func TestDetailSections(t *testing.T) {
	a, _, _ := openDemo(t, redesignPlan, 120, 40)
	order := func(text string, h2s ...string) {
		t.Helper()
		at := -1
		for _, h := range h2s {
			i := strings.Index(text, "\n"+h+" ─")
			if i <= at {
				t.Fatalf("%s out of order in\n%s", h, text)
			}
			at = i
		}
	}
	detail := func(key string) string {
		return viewText(taskDetail(a.doc, key, 78, true))
	}
	pin := detail("page-titles")
	order(pin, "DOES", "VERIFY", "FILES", "CONTEXT")
	for _, want := range []string{"\n  [PINNED] active · under chrome\n", "\n  Page titles and footer keys\n", "VERIFY ─", "─ 2\n", "─ 3\n", "  • go test ./internal/tui -run PageTitle", "  internal/tui/outline.go\n",
		"  needs   the tab bar on row 0", "  after   ✓ tab-bar  Two tabs on row 0, the active one filled", "  then    · agent-stream  Fold runs of thoughts", "  under   chrome  Chrome: tabs, page titles, keys", "  branch  feature/page_titles"} {
		if !strings.Contains(pin, want) {
			t.Errorf("the pin's detail has %q\n%s", want, pin)
		}
	}
	done := detail("tab-bar")
	order(done, "DONE", "INTENDED", "CONTEXT")
	for _, want := range []string{"\n  ✓ done · under chrome\n", "  did      Two tabs on row 0, the active one filled", "  changed  internal/tui/chrome.go", "  checked  go test ./internal/tui -run TabBar", "  notes    the cue cell is fixed width", "  does     Both tabs on row 0", "  then     ▸ page-titles  Page titles and footer keys"} {
		if !strings.Contains(done, want) {
			t.Errorf("a done task's detail has %q\n%s", want, done)
		}
	}
	for key, want := range map[string]string{
		"agent-stream": "  · waiting · after page-titles\n",
		"pin-card":     "  ! blocked · under plan-views\n",
		"width-table":  "  – skipped\n",
		"chrome":       "  1/2 done\n",
	} {
		if got := detail(key); !strings.Contains(got, want) {
			t.Errorf("%s's status line is %q\n%s", key, want, got)
		}
	}
	about := viewText(planDetail(a.doc, "rex/plans/ux/redesign.rex.json", 78, true))
	order(about, "GOAL", "SCOPE", "STACK", "DONE")
	for _, want := range []string{"\n  Rex visual redesign\n", "\n  active · 2 of 6 done  ━━━━━━━─────────────\n", "\n  rex/plans/ux/redesign.rex.json", "  in   • chrome\n       • outline\n  out  • extra palettes\n", "  base    dev", "─ 1 · 1 skipped\n  ✓ tab-bar  Two tabs on row 0, the active one filled"} {
		if !strings.Contains(about, want) {
			t.Errorf("About has %q\n%s", want, about)
		}
	}
	// A task the agent has not set done yet, with completion fields: what
	// it did so far, never DONE.
	sofar, _, _ := openDemo(t, `{"tasks": [{"id": "a", "title": "A", "status": "active", "does": ["the work"], "accomplished": ["half of it"], "verified_how": ["ran it once"]}]}`, 120, 40)
	if got := viewText(taskDetail(sofar.doc, "a", 78, true)); !strings.Contains(got, "\nSO FAR ─") || strings.Contains(got, "\nDONE ─") ||
		!strings.Contains(got, "  did      half of it") || !strings.Contains(got, "  checked  ran it once") {
		t.Errorf("an active task's completion fields are SO FAR\n%s", got)
	} else {
		order(got, "DOES", "SO FAR")
	}
	// A next up pin's status says so; under 16 rows details have no blank
	// rows (docs/ux.md §12).
	next, _, _ := openDemo(t, strings.Replace(demoPlan, `"status": "active",
      "depends_on"`, `"status": "pending",
      "depends_on"`, 1), 80, 12)
	got := viewText(taskDetail(next.doc, "card-highlight", 38, false))
	if !strings.HasPrefix(got, "  [NEXT UP] pending\n") || strings.Contains(got, "\n\n") {
		t.Fatalf("%s", got)
	}
}

// docs/ux.md §3, §6: PgUp and PgDn move the selection by the task rows the
// window shows under the card, so the first page, from the pin or the
// title row, lands on the first task below the window and no page skips
// a task the window never showed.
func TestOutlinePagesByItsWindow(t *testing.T) {
	tasks := []string{`{"id": "t00", "title": "Task 0", "status": "active", "depends_on": ["d"], "needs": ["a need"],
		"does": ["one thing", "another thing that runs long enough to wrap"], "verify": ["check one", "check two"], "files": ["internal/tui/outline.go"]}`}
	for i := 1; i < 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d"}`, i, i))
	}
	tasks = append(tasks, `{"id": "d", "title": "Done", "status": "done", "accomplished": ["the base"]}`)
	src := `{"title": "Big", "tasks": [` + strings.Join(tasks, ",") + `]}`
	// shown is where in outlineRows the window's task rows are.
	shown := func(a *app) []int {
		view := a.composeOptions().PlanView
		list := view[headOf(view):]
		var at []int
		for i, l := range list {
			if l.Kind == "gap" || l.Kind == "idle" || (l.Selected && i > 0 && list[i-1].Selected) {
				continue
			}
			at = append(at, slices.Index(outlineRows(a.doc.plan), strings.Fields(strings.TrimLeft(l.Text, "▌ "))[1]))
		}
		return at
	}
	for _, cols := range []int{80, 120} {
		a, _, _ := openDemo(t, src, cols, 24)
		rows := outlineRows(a.doc.plan)
		win := shown(a)
		if body := planBody(a); a.outline.sel != "t00" || win[0] != 1 || !strings.Contains(body, "│ done") {
			t.Fatalf("%d cols: the whole card over the list's top, sel %q\n%s", cols, a.outline.sel, body)
		}
		seen := map[int]bool{}
		for n := 0; a.outline.sel != rows[len(rows)-1]; n++ {
			for _, i := range win {
				seen[i] = true
			}
			press(a, "pagedown")
			next := shown(a)
			if n == 0 && a.outline.sel != rows[win[len(win)-1]+1] {
				t.Errorf("%d cols: the first page lands on %s, the first task under %v", cols, a.outline.sel, win)
			}
			if next[0] > win[len(win)-1]+1 || n > len(rows) {
				t.Fatalf("%d cols: a page skipped from %v to %v", cols, win, next)
			}
			win = next
		}
		for a.outline.sel != "" {
			for _, i := range win {
				seen[i] = true
			}
			press(a, "pageup")
			next := shown(a)
			if next[len(next)-1] < win[0]-1 {
				t.Fatalf("%d cols: a page up skipped from %v to %v", cols, win, next)
			}
			win = next
		}
		if len(seen) != len(rows)-1 {
			t.Errorf("%d cols: paging showed %d of %d tasks", cols, len(seen), len(rows)-1)
		}
	}
	// From the title row, above the window, a page lands on the first task
	// under the window too.
	for _, size := range [][2]int{{80, 12}, {80, 24}, {120, 12}, {80, 7}} {
		a, _, _ := openDemo(t, src, size[0], size[1])
		rows := outlineRows(a.doc.plan)
		press(a, "home")
		win := shown(a)
		if press(a, "pagedown"); win[0] != 1 || a.outline.sel != rows[win[len(win)-1]+1] {
			t.Errorf("%d×%d: PgDn from the title row lands on %s, the window held %v", size[0], size[1], a.outline.sel, win)
		}
	}
}

// docs/ux.md §6: a value keeps the file's line breaks, so a description's
// paragraphs and lists, notes and the goal read as written; a capped field
// takes the rest into its last row, cut with "…".
func TestDetailsKeepLineBreaks(t *testing.T) {
	p, err := plan.Parse([]byte(`{"title": "T", "goal": "Goal line one.\nGoal line two.", "tasks": [
		{"id": "a", "title": "A", "description": "para one.\n\n- item\n- item two", "needs": ["one\ntwo\nthree four five six seven eight nine ten eleven twelve"]},
		{"id": "b", "title": "B", "status": "done", "accomplished": ["did it"], "completed_notes": "note one.\n\nnote two."}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &planDoc{path: "rex/plans/x.rex.json", plan: p}
	trim := regexp.MustCompile(` +\n`)
	text := func(lines []LogLine) string { return trim.ReplaceAllString(viewText(lines), "\n") }
	a := text(taskDetail(d, "a", 60, false))
	for _, want := range []string{"─\n  para one.\n  - item\n  - item two\n", "\n  needs   one\n          two three four five six seven eight nine ten elev…\n"} {
		if !strings.Contains(a, want) {
			t.Errorf("task detail has %q\n%s", want, a)
		}
	}
	if b := text(taskDetail(d, "b", 60, false)); !strings.Contains(b, "\n  notes   note one.\n          note two.\n") {
		t.Errorf("notes keep their paragraphs\n%s", b)
	}
	if about := text(planDetail(d, "rex/plans/x.rex.json", 60, false)); !strings.Contains(about, "─\n  Goal line one.\n  Goal line two.\n") {
		t.Errorf("the goal keeps its lines\n%s", about)
	}
}

// docs/ux.md §6, §11: the card's done row is what the pin builds on: its
// last done dependency, said what it accomplished or not, not the plan's
// last done task, which stands in only when the pin depends on nothing
// done; the plan list's accordion names the same task. With nothing done
// there is no done row. verify takes 2 rows at most, then +N, as files
// does; a long id keeps the title inside the box.
func TestCardFields(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks": [
		{"id": "x", "title": "X", "status": "done", "accomplished": ["built x"]},
		{"id": "y", "title": "Y", "status": "done", "accomplished": ["built y"]},
		{"id": "pin", "title": "Pin", "status": "active", "depends_on": ["x"], "does": ["the work"], "verify": ["check one", "check two", "check three"]}]}`, 120, 32)
	if body := planBody(a); !strings.Contains(body, "│ done    ✓ x  built x") || strings.Contains(body, "✓ y  built y") ||
		!strings.Contains(body, "│ verify  check one ") || !strings.Contains(body, "│         check two  +1 ") || strings.Contains(body, "check three") {
		t.Errorf("the pin builds on x; two verify rows, then how many more\n%s", body)
	}
	// The done dependency names what the pin builds on even before it says
	// what it accomplished, never an unrelated task done later.
	early, _, _ := openDemo(t, `{"tasks": [
		{"id": "x", "title": "Schema migration", "status": "done"},
		{"id": "y", "title": "Unrelated docs", "status": "done", "accomplished": ["fixed README typos"]},
		{"id": "pin", "title": "Pin", "status": "active", "depends_on": ["x"], "does": ["the work"]}]}`, 120, 32)
	if body := planBody(early); !strings.Contains(body, "│ done    ✓ x  Schema migration ") || strings.Contains(body, "│ done    ✓ y") {
		t.Errorf("the pin builds on x, done without accomplished yet\n%s", body)
	}
	// The card and the plan list's accordion name the same task: the pin's
	// done dependency, though another task is done later in the file.
	same, _, _ := openDemo(t, `{"tasks": [
		{"id": "x", "title": "X", "status": "done", "accomplished": ["built x"]},
		{"id": "y", "title": "Y", "status": "done", "accomplished": ["built y"]},
		{"id": "pin", "title": "Pin", "status": "active", "depends_on": ["x"], "does": ["the work"]}]}`, 120, 32)
	if body := planBody(same); !strings.Contains(body, "│ done    ✓ x  built x ") {
		t.Errorf("the card names x\n%s", body)
	}
	press(same, "esc")
	if row := same.plans.Rows[same.plans.Cursor]; row.Group != groupOpen {
		t.Fatalf("cursor on %+v", row)
	}
	if body := planBody(same); !strings.Contains(body, "done    ✓ x  built x") || strings.Contains(body, "✓ y") {
		t.Errorf("OPEN's accordion names x, as the card does\n%s", body)
	}
	b, _, _ := openDemo(t, `{"tasks": [{"id": "x", "title": "X", "status": "done"}, {"id": "y", "title": "Y", "status": "active", "does": ["the work"]}]}`, 120, 32)
	if body := planBody(b); !strings.Contains(body, "│ done    ✓ x  X") {
		t.Errorf("no done dependency: the plan's last done task\n%s", body)
	}
	c, _, _ := openDemo(t, `{"tasks": [{"id": "pin", "title": "Pin", "status": "active", "does": ["the work"]}]}`, 120, 32)
	if body := planBody(c); strings.Contains(body, "│ done") || !strings.Contains(body, "│ does    the work") {
		t.Errorf("nothing done, no done row\n%s", body)
	}
	// A parent's done is only a label: depending on one names the plan's
	// last done task, as About's DONE and the plan list do.
	parent, _, _ := openDemo(t, `{"tasks": [
		{"id": "p", "title": "P", "status": "done", "accomplished": ["parent acc"]},
		{"id": "k", "title": "K", "parent": "p", "status": "done", "accomplished": ["kid acc"]},
		{"id": "z", "title": "Z", "status": "active", "depends_on": ["p"], "does": ["the work"]}]}`, 120, 32)
	if body := planBody(parent); !strings.Contains(body, "│ done    ✓ k  kid acc") || strings.Contains(body, "✓ p") {
		t.Errorf("a parent dependency falls through to the last done leaf\n%s", body)
	}
	long, _, _ := openDemo(t, `{"tasks": [{"id": "a-really-long-task-identifier-that-overflows", "title": "Short title", "status": "active", "does": ["x"]}]}`, 102, 32)
	if body := planBody(long); !strings.Contains(body, "╭─[PINNED]─ a-really-long-task-identifier-that-overflows ─") || !strings.Contains(body, "\n│ Short title ") {
		t.Errorf("a long id leaves the title to a row inside\n%s", body)
	}
	// files keeps its count when even the first name doesn't fit.
	for _, tc := range []struct {
		files []string
		room  int
		want  string
	}{
		{[]string{"internal/tui/session_handler_impl.go", "a.go", "b.go"}, 18, "session_handl…  +2"},
		{[]string{"internal/tui/session_handler_impl.go"}, 18, "session_handler_impl.go"},
		{[]string{"internal/tui/plans.go", "internal/tui/live.go", "internal/tui/plans_test.go"}, 26, "plans.go  live.go  +1"},
	} {
		if got := filesLine(tc.files, tc.room); got != tc.want || (len(tc.files) > 1 && cellLen(got) > tc.room) {
			t.Errorf("filesLine(%q, %d) = %q, want %q", tc.files, tc.room, got, tc.want)
		}
	}
	narrow, _, _ := openDemo(t, `{"tasks": [{"id": "a", "title": "A", "status": "active", "files": ["internal/tui/session_handler_impl.go", "a.go", "b.go"]}]}`, 72, 24)
	if body := planBody(narrow); !strings.Contains(body, "│ files   session_handl…  +2 │") {
		t.Errorf("a 30-cell card keeps +2\n%s", body)
	}
}

// docs/ux.md §5, §6: the card is in the pin's yellow, its box and its id
// (bold), its title bold text, its done row quiet; ✓ and – rows, parents'
// rollups and waits are muted, the pinned tag yellow, a blocked row red
// with its tag at every width, the goal quiet; the details' titles are
// bold.
func TestCardStyle(t *testing.T) {
	role := func(l LogLine, s Span) string {
		if s.Role != "" {
			return s.Role
		}
		return l.Kind
	}
	span := func(l LogLine, text string) (Span, bool) {
		for _, s := range l.Spans {
			if strings.TrimSpace(s.Text) == text {
				return s, true
			}
		}
		return Span{}, false
	}
	for _, cols := range []int{80, 120} {
		a, _, _ := openDemo(t, redesignPlan, cols, 32)
		view := a.composeOptions().PlanView
		head := headOf(view)
		found := map[string]bool{}
		for i, l := range view {
			text := strings.TrimRight(l.Text, " ")
			if i < head && strings.ContainsAny(text, "╭│╰") {
				for _, s := range l.Spans {
					if strings.ContainsAny(s.Text, "╭╮│╰╯─") && s.Fill == "" && role(l, s) != "title" {
						t.Errorf("%d cols: the card's edge is yellow: %q in %q", cols, s.Text, text)
					}
				}
				if s, ok := span(l, "page-titles"); ok && (s.Role != "title" || !s.Bold) {
					t.Errorf("%d cols: the card's id is bold yellow: %+v", cols, s)
				}
				if s, ok := span(l, "Page titles and footer keys"); ok {
					found["title"] = true
					if !s.Bold || role(l, s) != "text" {
						t.Errorf("%d cols: the card's title is bold text: %+v", cols, s)
					}
				}
				if strings.HasPrefix(text, "│ done") {
					found["done"] = true
					if k := slices.IndexFunc(l.Spans, func(s Span) bool { return strings.HasPrefix(s.Text, "✓ tab-bar") }); k < 0 || role(l, l.Spans[k]) != "muted" {
						t.Errorf("%d cols: the done row is quiet: %+v", cols, l.Spans)
					}
				}
				continue
			}
			if strings.HasPrefix(text, "  A newcomer") {
				found["goal"] = true
				if l.Kind != "muted" {
					t.Errorf("%d cols: the goal is quiet: %+v", cols, l)
				}
			}
			if i < head {
				continue
			}
			name := strings.Fields(strings.TrimLeft(text, "▌ "))
			if len(name) < 2 {
				continue
			}
			last := l.Spans[len(l.Spans)-1]
			switch id := name[1]; {
			case id == "tab-bar" || id == "width-table":
				found[id] = true
				if l.Kind != "muted" || slices.ContainsFunc(l.Spans, func(s Span) bool { return role(l, s) != "muted" }) {
					t.Errorf("%d cols: done and skipped rows are quiet: %+v", cols, l)
				}
			case name[0] == "chrome":
				found["rollup"] = true
				if last.Text != "1/2" || role(l, last) != "muted" {
					t.Errorf("%d cols: a parent's rollup is muted: %+v", cols, last)
				}
			case id == "agent-stream" && cols == 120:
				found["after"] = true
				if last.Text != "after page-titles" || role(l, last) != "muted" {
					t.Errorf("%d cols: a wait is muted: %+v", cols, last)
				}
			case id == "pin-card":
				found["blocked"] = true
				if l.Kind != "error" || last.Text != "blocked" || role(l, last) != "error" {
					t.Errorf("%d cols: a blocked row is red and tagged at every width: %+v", cols, l)
				}
			case id == "page-titles" && cols == 120:
				found["pinned"] = true
				if last.Text != "pinned" || role(l, last) != "title" {
					t.Errorf("%d cols: the pinned tag is yellow: %+v", cols, last)
				}
			}
		}
		for _, k := range []string{"title", "done", "goal", "tab-bar", "width-table", "rollup", "blocked"} {
			if !found[k] {
				t.Errorf("%d cols: no %s row\n%s", cols, k, a.frame())
			}
		}
		if cols == 120 && (!found["after"] || !found["pinned"]) {
			t.Errorf("%d cols: no tags\n%s", cols, a.frame())
		}
	}
	a, _, _ := openDemo(t, redesignPlan, 120, 40)
	for _, tc := range []struct {
		lines []LogLine
		title string
	}{
		{taskDetail(a.doc, "page-titles", 78, true), "Page titles and footer keys"},
		{taskDetail(a.doc, "tab-bar", 78, true), "Tab bar on row 0"},
		{planDetail(a.doc, "rex/plans/ux/redesign.rex.json", 78, true), "Rex visual redesign"},
	} {
		i := slices.IndexFunc(tc.lines, func(l LogLine) bool { return strings.TrimSpace(l.Text) == tc.title })
		if i < 0 {
			t.Fatalf("no title %q\n%s", tc.title, viewText(tc.lines))
		}
		if s, _ := span(tc.lines[i], tc.title); !s.Bold || role(tc.lines[i], s) != "text" {
			t.Errorf("a detail's title is bold text: %+v", tc.lines[i])
		}
	}
}

// docs/ux.md §6: a plan's text can be as long as a plan file (4 MB): the
// outline, task detail and About lay out in time linear in it, never
// quadratic, so an agent that pastes a log into a description never
// freezes Rex.
func TestLongPlanTextLaysOutFast(t *testing.T) {
	long := strings.Repeat("word ", 2<<20/5)
	for _, field := range []string{"description", "does", "verify", "goal"} {
		task := map[string]any{"id": "a", "title": "A", "status": "active"}
		doc := map[string]any{"title": "P", "tasks": []any{task, map[string]any{"id": "b", "title": "B"}}}
		switch field {
		case "goal":
			doc["goal"] = long
		case "description":
			task[field] = long
		default:
			task[field] = []string{long}
		}
		src, _ := json.Marshal(doc)
		a, _, _ := openDemo(t, string(src), 80, 24)
		if a.doc.plan == nil {
			t.Fatalf("2 MB %s: %s", field, a.doc.err)
		}
		for _, screen := range [][]string{nil, {"\r"}, {"esc", "home", "\r"}, {"esc", "end"}} {
			press(a, screen...)
			start := time.Now()
			a.frameRows()
			if took := time.Since(start); took > time.Second {
				t.Errorf("2 MB %s: a frame after %v takes %v", field, screen, took)
			}
		}
	}
	start := time.Now()
	rows := paraRows(long+long, 26, 2)
	if took := time.Since(start); took > time.Second || len(rows) != 2 || !strings.HasSuffix(rows[1], "…") {
		t.Errorf("paraRows held to 2 rows of 4 MB: %v, %q", took, rows)
	}
	if rows := wrapMost(long, 26, 3); len(rows) != 3 || rows[0] != "word word word word word" || !strings.HasSuffix(rows[2], "…") {
		t.Errorf("wrapMost held to 3 rows: %q", rows)
	}
}

// docs/ux.md §6: the outline's body is the outline, even when nothing of
// it fits: an empty read-only plan with problems at 7 rows keeps the first
// rows of its warnings, and the plan list never shows through under the
// outline's page title.
func TestOutlineNeverDrawsThePlanList(t *testing.T) {
	for _, src := range []string{
		`{"version": 2, "title": "Newer", "goal": "a goal", "tasks": []}`,
		`{"version": 2, "title": "Newer", "goal": "a goal", "status": "weird", "tasks": []}`,
		`{"title": "No version", "goal": "a goal", "tasks": []}`,
		`{"version": 1, "title": "Stub", "status": "weird", "tasks": [], "scope": "oops"}`,
		`{"version": 1, "title": "Tasks as an object", "goal": "ship it", "tasks": {"a": {"title": "A"}}}`,
	} {
		for _, size := range [][2]int{{80, 7}, {120, 7}, {24, 7}, {80, 8}} {
			a, _, _ := openDemo(t, src, size[0], size[1])
			body := planBody(a)
			if strings.Contains(body, "cuj/demo") || strings.Contains(body, "no tasks yet · the") || strings.Contains(body, "OPEN ─") || strings.TrimSpace(body) == "" {
				t.Errorf("%v: the outline's body, not the plan list's\n%s\nplan %s", size, a.frame(), src)
			}
			if p := a.doc.plan; p.ReadOnly != "" && !strings.Contains("\n"+body, "\n  read-only: ") {
				t.Errorf("%v: a read-only plan says why\n%s", size, body)
			}
		}
	}
}

// docs/ux.md §6: the outline holds still as the cursor moves. Whether the
// list has blank rows between groups and whether it scrolls are decided
// without the selection's blurb row, so moving ↓ and ↑ through every row
// never changes the head, the blank rows, or a list that fits; ↓ more
// marks a list that goes on below wherever a row is left for a marker,
// ↑ more never takes the selection's margin row, and a frame drawn again
// with nothing moved is the same frame.
func TestOutlineHoldsStillAsTheCursorMoves(t *testing.T) {
	var done7 []string
	for i := 0; i < 7; i++ {
		done7 = append(done7, fmt.Sprintf(`{"id": "t%d", "title": "Task %d", "status": "done", "accomplished": ["did %d"]}`, i, i, i))
	}
	plans := map[string]string{
		"cuj":      string(cujFixture(t)),
		"redesign": redesignPlan,
		"done7":    `{"title": "P", "tasks": [` + strings.Join(done7, ",") + `]}`,
	}
	for name, src := range plans {
		for _, cols := range []int{80, 120} {
			a, _, _ := openDemo(t, src, cols, 24)
			rows := outlineRows(a.doc.plan)
			n, last := len(rows)-1, rows[len(rows)-1]
			for lines := 7; lines <= 30; lines++ {
				a.settle(cols, lines)
				bodyH := a.layout().bodyH
				want := wantRows(bodyH, n)
				type shape struct {
					head       string
					gaps       int
					fits, more bool
				}
				var first shape
				press(a, "home")
				for step := 0; step < 2*len(rows)-1; step++ {
					if step > 0 {
						key := "down"
						if step >= len(rows) {
							key = "up"
						}
						press(a, key)
					}
					where := fmt.Sprintf("%s %d×%d on %q", name, cols, lines, a.outline.sel)
					for _, view := range []int{a.outline.view, 1 << 20} { // the window where it was, and left past the list's end
						a.outline.view = view
						if frame := a.frame(); a.frame() != frame {
							t.Errorf("%s: the next frame differs with nothing moved\n%s\n%s", where, frame, a.frame())
						}
					}
					view := a.composeOptions().PlanView
					h := headOf(view)
					var s shape
					for _, l := range view[:h] {
						if l.Kind == "h2" {
							s.head += "TASKS\n" // its meta is where the cursor is
						} else {
							s.head += l.Text + "\n"
						}
					}
					list := view[h:]
					below, blurb := true, 0
					for i, l := range list {
						switch {
						case l.Kind == "gap":
							s.gaps++
						case l.Text == "  ↓ more" || l.Text == "  ↑ more":
							s.more = true
							if l.Text == "  ↑ more" && n >= 3 && i+1 < len(list) && list[i+1].Selected {
								t.Errorf("%s: ↑ more takes the selection's margin row\n%s", where, a.frame())
							}
						case l.Selected && i > 0 && list[i-1].Selected:
							blurb = 1
						case strings.Contains(l.Text, " "+last+" "):
							below = false
						}
					}
					s.fits = !s.more && taskRows(list) == n
					if below && !strings.Contains(viewText(list), "↓ more") && bodyH-h-want-blurb >= 1 {
						t.Errorf("%s: tasks below the window and a row for ↓ more, but no ↓ more\n%s", where, a.frame())
					}
					if step == 0 {
						first = s
						continue
					}
					if s.head != first.head || s.gaps != first.gaps || s.fits != first.fits {
						t.Errorf("%s: the layout moved with the cursor: head %v, gaps %d → %d, fits %v → %v\n%s", where, s.head == first.head, first.gaps, s.gaps, first.fits, s.fits, a.frame())
						break
					}
				}
			}
		}
	}
}

// docs/ux.md §4, §6: a short outline still says that its list scrolls and
// where the cursor is in it: TASKS's meta while TASKS is on screen, else
// the page title's, "active · 16 of 30", just the position where that
// doesn't fit. On the title row the list starts at its top and ↓ more
// takes its last row.
func TestShortOutlineSaysWhereItIs(t *testing.T) {
	var ts []string
	for i := 1; i <= 30; i++ {
		ts = append(ts, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d", "does": ["does %d"]}`, i, i, i))
	}
	src := func(title string) string {
		return `{"title": "` + title + `", "status": "active", "tasks": [` + strings.Join(ts, ",") + `]}`
	}
	for _, tc := range []struct {
		cols, rows int
		tasks      bool // TASKS is on screen
	}{{80, 7, false}, {80, 8, false}, {120, 8, false}, {80, 12, true}} {
		a, _, _ := openDemo(t, src("Many"), tc.cols, tc.rows)
		a.outline.sel = "t16"
		words := func() string { return strings.Join(strings.Fields(textOf(a.frameRows().Rows[2], tc.cols)), " ") }
		body, title := planBody(a), words()
		switch {
		case strings.Contains(body, "TASKS ─") != tc.tasks:
			t.Errorf("%d×%d: TASKS on screen %v\n%s", tc.cols, tc.rows, !tc.tasks, body)
		case tc.tasks && (!strings.Contains(body, "─ 16 of 30") || title != "PLANS › Many active"):
			t.Errorf("%d×%d: TASKS says where the cursor is: %q\n%s", tc.cols, tc.rows, title, body)
		case !tc.tasks && title != "PLANS › Many active · 16 of 30":
			t.Errorf("%d×%d: with TASKS gone the page title says where the cursor is: %q\n%s", tc.cols, tc.rows, title, body)
		}
		press(a, "home")
		body, title = planBody(a), words()
		lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
		if lines[len(lines)-1] != "  ↓ more" || tc.tasks && !strings.Contains(body, "─ 0/30 done") || title != "▌ PLANS › Many active" {
			t.Errorf("%d×%d: the title row: the list from its top, then ↓ more: %q\n%s", tc.cols, tc.rows, title, body)
		}
	}
	a, _, _ := openDemo(t, src("Thirty tasks"), 80, 7)
	a.outline.sel = "t16"
	if title := strings.Join(strings.Fields(textOf(a.frameRows().Rows[2], 80)), " "); title != "PLANS › Thirty tasks 16 of 30" {
		t.Errorf("the position, not the status, where both don't fit: %q", title)
	}
}

// docs/ux.md §6: the ladder leaves out nothing that fits. Once a big step
// (the compact card, the strip, the warnings) frees rows, each smaller
// piece that fits again comes back, so undoing any step it kept would
// not fit: the CUJ plan's goal sits over the compact card at 80×13, and a
// read-only plan whose warnings don't fit keeps its goal, strip and TASKS.
func TestLadderGivesBackWhatFits(t *testing.T) {
	var six []string
	for i := 1; i <= 6; i++ {
		six = append(six, fmt.Sprintf(`{"id": "t%02d", "title": "Task %d", "does": ["step %d"]}`, i, i, i))
	}
	v2 := `{"version": 2, "title": "V2", "goal": "g", "status": "weird", "tasks": [{"id": "x", "title": "X", "status": "active", "does": ["x things"], "depends_on": ["nope"], "parent": "ghost"}, {"id": "y", "title": "Y", "status": "odd", "depends_on": ["y"]}]}`
	plans := map[string]string{
		"cuj":      string(cujFixture(t)),
		"redesign": redesignPlan,
		"warned":   `{"version": 2, "title": "Warned", "goal": "a goal", "tasks": [{"id": "a", "status": "active", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]},` + strings.Join(six, ",") + `]}`,
		"v2":       v2,
	}
	for name, src := range plans {
		for _, cols := range []int{24, 80, 120} { // 24, 38 and 78 text cells
			a, _, _ := openDemo(t, src, cols, 24)
			for lines := 7; lines <= 30; lines++ {
				a.settle(cols, lines)
				l := a.layout()
				o := newOutline(a.doc.plan, l.textW)
				want := wantRows(l.bodyH, o.rows)
				on, head := o.collapse(l.bodyH, want)
				for i, kept := range on {
					if !kept {
						continue
					}
					back := slices.Clone(on)
					back[i] = false
					if h := o.head(o.fitFor(back)); len(h) > len(head) && len(h)+want <= l.bodyH {
						t.Errorf("%s %d×%d: ladder step %d is kept, but its piece fits: %d head rows and %d task rows in %d\n%s", name, cols, lines, i+1, len(h), want, l.bodyH, planBody(a))
					}
				}
			}
		}
	}
	c, _, _ := cujApp(t)
	if c.settle(80, 13); !strings.HasPrefix(planBody(c), "  A teammate opens this plan in Rex a…\n╭─[PINNED]─ card-highlight ─") {
		t.Errorf("80×13: the goal fits over the compact card\n%s", planBody(c))
	}
	w, _, _ := openDemo(t, v2, 80, 9)
	if body := planBody(w); body != "  read-only: version 2 · newer than\n    this Rex reads\n[PINNED] x  X\n▌ ▸ x X\n  · y Y                          cycle" {
		t.Errorf("80×9: the problems don't fit; read-only outranks the goal and stays over the strip\n%s", body)
	}
}

// docs/ux.md §6: values wrap at spaces and, as CJK text does, after a
// wide glyph, so wide text fills its rows; a run of narrow letters breaks
// only when it is longer than the row, and an emoji sequence never splits.
func TestWrapBreaksWideText(t *testing.T) {
	for _, tc := range []struct {
		text        string
		width, most int
		want        []string
	}{
		{"全角の値 🦖 を枠の中に入れて、はみ出さないように折り返す", 20, 2, []string{"全角の値 🦖 を枠の中", "に入れて、はみ出さ…"}},
		{"漢字abcdef ghi", 6, 0, []string{"漢字", "abcdef", "ghi"}},
		{"see 漢字 then internationalization", 10, 0, []string{"see 漢字", "then", "internatio", "nalization"}},
		{"👍🏽👍🏽👍🏽 👨‍💻👨‍💻", 4, 0, []string{"👍🏽👍🏽", "👍🏽", "👨‍💻👨‍💻"}},
	} {
		got := wrapMost(tc.text, tc.width, tc.most)
		if !slices.Equal(got, tc.want) {
			t.Errorf("wrapMost(%q, %d, %d) = %q, want %q", tc.text, tc.width, tc.most, got, tc.want)
		}
		for _, row := range got {
			if cellLen(row) > tc.width {
				t.Errorf("%q is wider than %d", row, tc.width)
			}
		}
	}
}
