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

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
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

// openDemo opens src as cuj/demo.rex.json in a fresh app at cols×rows,
// from the plan list (latched), its Opened notice gone.
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
		t.Fatalf("not on the task list: at=%d %q", a.at, a.plans.Err)
	}
	// The Opened notice has had its time: what a test does next shows on
	// the notice row (TestOpenedNotice has it).
	c.t = c.t.Add(toastFor)
	a.tick()
	return a, c, path
}

// later moves the clock past a poll and ticks.
func later(a *app, c *clock) {
	c.t = c.t.Add(docPollEvery + time.Millisecond)
	a.tick()
}

// settled is later, then past creditWait: a change the poll read that no
// agent's write claimed is announced as the plan's (docs/ux.md §7b).
func settled(a *app, c *clock) {
	later(a, c)
	c.t = c.t.Add(creditWait)
	a.tick()
}

// docs/ux.md §4, §6: row 0 names the plan, its progress and (picked in
// the list, latched) no follow word; row 1 is the pin line; the body is

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

// planBody is a frame's body: the text column under rows 0 and 1 (and
// row 2, blank from blankFrom rows) and over the footer (and the notice
// row, from blankFrom rows) (docs/ux.md §4).
func planBody(a *app) string {
	rows := a.frameRows().Rows
	top, bottom := 2, len(rows)-1
	if a.lines >= blankFrom {
		top, bottom = 3, len(rows)-2
	}
	var out []string
	for _, r := range rows[top:bottom] {
		out = append(out, textOf(r, a.columns))
	}
	return strings.Join(out, "\n")
}

// manyTasks is a plan of n tasks, t00 on, too many for most screens: the
// first is the agent's pin, every third is done.
func manyTasks(n int) string {
	var tasks []string
	for i := 0; i < n; i++ {
		status := "pending"
		switch {
		case i == 0:
			status = "active"
		case i%3 == 0:
			status = "done"
		}
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "title": "Task number %d", "status": %q}`, i, i, status))
	}
	return `{"version": 1, "title": "Many", "goal": "a long list", "tasks": [` + strings.Join(tasks, ", ") + `]}`
}

// specPlan is the redesign spec's sample plan (docs/ux.md §6): a done
// task, the agent's pin, what waits on it, a blocked and a skipped task,
// a stack on dev.
const specPlan = `{"version": 1, "title": "Rex visual redesign", "goal": "A newcomer always knows which page they are on, what is pinned, and which key gets them where.", "status": "active", "base": "dev",
 "scope": {"in": ["chrome", "task list", "plan list"], "out": ["extra palettes", "web UI"]},
 "tasks": [
 {"id": "tab-bar", "title": "Tab bar on row 0", "status": "done", "does": ["Two places on row 0, the one on screen marked"], "accomplished": ["Two tabs on row 0, the active one filled", "Cue cell is fixed width"], "modified": ["internal/tui/chrome.go", "internal/tui/chrome_test.go"], "verified_how": ["go test ./internal/tui -run TabBar (passed)", "tmux at 80x24 and 120x32"], "completed_notes": "The cue cell must stay one cell wide; width_test pins it.", "branch": "feature/tab_bar"},
 {"id": "page-titles", "title": "Page titles and footer keys", "status": "active", "depends_on": ["tab-bar"], "needs": ["the tab bar on row 0"], "does": ["Every screen opens with a title row that says where you are", "The footer lists keys only, in words"], "verify": ["go test ./internal/tui -run PageTitle", "tmux at 80x24: every screen names itself", "NO_COLOR frame matches"], "files": ["internal/tui/chrome.go", "internal/tui/outline.go", "internal/tui/plans.go"], "scope": {"out": ["web UI"]}, "branch": "feature/page_titles"},
 {"id": "agent-stream", "title": "Fold runs of thoughts", "depends_on": ["page-titles"], "does": ["One row per run of thoughts on the Agent tab"], "files": ["internal/tui/stream.go"], "branch": "feature/agent_stream"},
 {"id": "plan-list", "title": "Plan list groups"},
 {"id": "pin-card", "title": "Pinned card on the outline"},
 {"id": "width-table", "title": "Glyph widths in width_test", "status": "done"},
 {"id": "help-page", "title": "Help lists every key", "status": "blocked"},
 {"id": "readme", "title": "README screenshots", "status": "skipped"}
 ]}`

// groupsPlan has two groups, a parent nested in one, and tasks after them
// that are in none (docs/ux.md §6).
const groupsPlan = `{"version": 1, "title": "Rex visual redesign", "status": "active", "tasks": [
 {"id": "chrome", "title": "Chrome"},
 {"id": "tab-bar", "title": "Tab bar on row 0", "parent": "chrome", "status": "done"},
 {"id": "page-titles", "title": "Page titles and footer keys", "parent": "chrome", "status": "active"},
 {"id": "footer-keys", "title": "Footer keys in words", "parent": "chrome"},
 {"id": "plan-views", "title": "Plan views"},
 {"id": "plan-list-p", "title": "Plan list", "parent": "plan-views"},
 {"id": "plan-list", "title": "Plan list groups", "parent": "plan-list-p"},
 {"id": "pin-card", "title": "Pinned card on the outline", "parent": "plan-list-p"},
 {"id": "help-page", "title": "Help lists every key", "parent": "plan-views", "status": "blocked"},
 {"id": "agent-stream", "title": "Fold runs of thoughts"},
 {"id": "width-table", "title": "Glyph widths in width_test", "status": "done"},
 {"id": "readme", "title": "README screenshots", "status": "skipped"}
 ]}`

// trimmed is s's lines with their trailing blanks off and trailing blank
// lines dropped.
func trimmed(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// docs/ux.md §6: the open plan is one task list. Row 0 is its title and
// progress, row 1 the pin line, then the tasks: the gutter, a glyph, the
// id in its column, the title and one tag; nothing else on the screen.
func TestTaskListAtAGlance(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 80, 24)
	rows := a.frameRows().Rows
	if got := textOf(rows[0], 80); got != sides("Rex visual redesign", "2/8 done   plan · agent", 80) {
		t.Errorf("row 0: %q", got)
	}
	if got := textOf(rows[1], 80); got != sides("pinned  page-titles · Page titles and footer keys", "not sent · ? help", 80) {
		t.Errorf("row 1, the pin line, and that no wired agent takes the pin: %q", got)
	}
	want := `  ✔ tab-bar       Tab bar on row 0
❯   page-titles   Page titles and footer keys                            pinned
    agent-stream  Fold runs of thoughts
    plan-list     Plan list groups
    pin-card      Pinned card on the outline
  ✔ width-table   Glyph widths in width_test
  ! help-page     Help lists every key
  – readme        README screenshots`
	if got := trimmed(planBody(a)); got != want {
		t.Errorf("the body is the list and nothing else:\n%s\nwant\n%s", got, want)
	}
	if got := textOf(rows[23], 80); got != "↑↓ select · enter details · p unpin · d done · esc plans · tab agent · ? help" {
		t.Errorf("footer %q", got)
	}
	if a.outline.sel != "page-titles" {
		t.Fatalf("the cursor lands on the pin: %q", a.outline.sel)
	}
	// Nothing previews the next screen: no card, goal, blurbs or TASKS.
	for _, never := range []string{"╭", "[PINNED]", "TASKS", "A newcomer always", " · Every screen", "▸", "○"} {
		if strings.Contains(a.frame(), never) {
			t.Errorf("the list has no %q\n%s", never, a.frame())
		}
	}
}

// docs/ux.md §5, §6: on the list only the pinned tag is yellow, a next up
// tag muted and also active red; done and skipped rows are muted with ✔
// green; the selection is ❯ and purple and keeps its tag's color; a group
// label is bold text that is never selected.
func TestTaskRowRoles(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks": [{"id": "c", "title": "Group"}, {"id": "a", "title": "A", "status": "active", "parent": "c"}, {"id": "b", "title": "B", "status": "active"}, {"id": "d", "title": "D", "status": "done"}, {"id": "s", "title": "S", "status": "skipped"}]}`, 80, 24)
	p := a.doc.plan
	l := newTaskList(p, 80, 19, true)
	role := func(line LogLine, text string) (string, bool) {
		for _, s := range line.Spans {
			if strings.TrimSpace(s.Text) == text {
				return s.Role, s.Bold
			}
		}
		t.Fatalf("no span %q in %+v", text, line)
		return "", false
	}
	for i, want := range []struct {
		text, kind string
	}{{"  Group", "text"}, {"❯     a     A", "pick"}, {"", "gap"}, {"    b       B", "text"}, {"  ✔ d       D", "muted"}, {"  – s       S", "muted"}} {
		line := l.draw(i, l.lines[i].t != nil && taskKey(l.lines[i].t) == a.outline.sel)
		if !strings.HasPrefix(line.Text, want.text) || line.Kind != want.kind {
			t.Errorf("line %d: %q kind %q, want %q kind %q", i, line.Text, line.Kind, want.text, want.kind)
		}
	}
	if r, bold := role(l.draw(0, false), "Group"); r != "text" || !bold {
		t.Errorf("a group label is bold text: %q %v", r, bold)
	}
	if r, _ := role(l.draw(1, true), "pinned"); r != "accent" {
		t.Errorf("the pinned tag keeps its yellow on the selection: %q", r)
	}
	if r, _ := role(l.draw(3, false), "also active"); r != "error" {
		t.Errorf("a second active task's tag is red: %q", r)
	}
	for _, s := range l.draw(1, true).Spans {
		if s.Role != "" && s.Role != "accent" {
			t.Errorf("the selection is purple but for its tag: %+v", s)
		}
	}
	next, _, _ := openDemo(t, strings.Replace(demoPlan, `"status": "active",
      "depends_on"`, `"status": "pending",
      "depends_on"`, 1), 80, 24)
	nl := newTaskList(next.doc.plan, 80, 19, true)
	if r, _ := role(nl.draw(1, false), "next up"); r != "muted" {
		t.Errorf("next up is muted, never the pin's yellow: %q", r)
	}
	// In color: the ✔ is green, the tag yellow, and nothing is filled.
	a.enabled = true
	frame := strings.Join(a.frameRows().Rows, "\n")
	if !strings.Contains(frame, mascot.FG(mascot.UI["accent"])+"pinned") || strings.Contains(frame, "\033[48") {
		t.Errorf("the pinned tag is yellow and nothing is filled\n%q", frame)
	}
}

// docs/ux.md §6: a top-level parent is a group label the cursor skips,
// with its progress; deeper parents have no line, their tasks sit in the
// group; a blank row goes above each group and before the ungrouped tasks
// after one, only when the whole list fits. Under 12 rows the labels go
// and the indent keeps the grouping.
func TestTaskListGroups(t *testing.T) {
	a, _, _ := openDemo(t, groupsPlan, 80, 24)
	want := `  Chrome                                                                    1/3
    ✔ tab-bar      Tab bar on row 0
❯     page-titles  Page titles and footer keys                           pinned
      footer-keys  Footer keys in words

  Plan views                                                                0/3
      plan-list    Plan list groups
      pin-card     Pinned card on the outline
    ! help-page    Help lists every key

    agent-stream   Fold runs of thoughts
  ✔ width-table    Glyph widths in width_test
  – readme         README screenshots`
	if got := trimmed(planBody(a)); got != want {
		t.Errorf("groups at 80×24:\n%s\nwant\n%s", got, want)
	}
	if keys := taskKeys(a.doc.plan); strings.Join(keys, " ") != "tab-bar page-titles footer-keys plan-list pin-card help-page agent-stream width-table readme" {
		t.Errorf("the cursor reaches the leaves only: %v", keys)
	}
	press(a, "home", "up")
	if a.outline.sel != "tab-bar" {
		t.Errorf("home and ↑ stop on the first task, under its label: %q", a.outline.sel)
	}
	press(a, "down", "down", "down")
	if a.outline.sel != "plan-list" {
		t.Errorf("↓ skips the label: %q", a.outline.sel)
	}
	if got := taskPlace(a.doc.plan, "plan-list"); got != "task 4 of 9 · in Plan views › Plan list" {
		t.Errorf("details name the nested parent: %q", got)
	}
	// Short: no labels, no blank rows, the indent stays.
	a.settle(80, 11)
	if body := planBody(a); strings.Contains(body, "Chrome") || strings.Contains(body, "\n\n") || !strings.Contains(body, "    ✔ tab-bar      Tab bar on row 0") {
		t.Errorf("under 12 rows\n%s", body)
	}
	// A list that scrolls has no blank rows, wherever the cursor is.
	b, _, _ := openDemo(t, groupsPlan, 80, 14)
	for i := 0; i < 9; i++ {
		if body := planBody(b); strings.Contains(strings.TrimRight(body, "\n "), "\n\n") || !strings.Contains(body, "Plan views") && !strings.Contains(body, "Chrome") {
			t.Fatalf("row %d: a list that scrolls has labels and no blank rows\n%s", i, body)
		}
		press(b, "down")
	}
}

// docs/ux.md §6: under 40 cells a row is its id and tag; from 40 the
// title clips before the tag goes.
func TestNarrowRowsAreIdAndTag(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 30, 12)
	if body := planBody(a); !strings.Contains(body, "❯   page-titles        pinned") || strings.Contains(body, "Page titles") || !strings.Contains(body, "  ✔ tab-bar\n") {
		t.Errorf("30 cells\n%s", body)
	}
	a.settle(44, 16)
	if body := planBody(a); !strings.Contains(body, "❯   page-titles   Page titles and …  pinned") || !strings.Contains(body, "    pin-card      Pinned card on the outli…") {
		t.Errorf("44 cells\n%s", body)
	}
	a.settle(24, 7)
	if body := planBody(a); !strings.Contains(body, "❯   page-titles  pinned") {
		t.Errorf("24 cells\n%s", body)
	}
	for _, row := range a.frameRows().Rows {
		if cellLen(row) > 24 {
			t.Fatalf("row overflows: %q", row)
		}
	}
}

// cursorKeys moves the cursor every way the task list takes.
var cursorKeys = []string{"down", "down", "down", "pagedown", "down", "pagedown", "end", "up", "pageup", "up", "up", "pageup", "home", "down", "end", "pageup", "pageup", "pageup", "home"}

// docs/ux.md §4.2: the cursor lands only on rows enter acts on: never a
// group label, a blank row or a scroll marker, with any key, at any size,
// and exactly one row has ❯.
func TestCursorNeverLandsOnALabel(t *testing.T) {
	var tasks []string
	for g := 0; g < 6; g++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "g%d", "title": "Group %d"}`, g, g))
		tasks = append(tasks, fmt.Sprintf(`{"id": "g%dn", "title": "Nested %d", "parent": "g%d"}`, g, g, g))
		for i := 0; i < 4; i++ {
			parent := fmt.Sprintf("g%d", g)
			if i%2 == 1 {
				parent += "n"
			}
			tasks = append(tasks, fmt.Sprintf(`{"id": "t%d%d", "title": "Task %d.%d", "parent": %q, "status": %q}`, g, i, g, i, parent, []string{"pending", "done", "blocked", "skipped"}[i]))
		}
		tasks = append(tasks, fmt.Sprintf(`{"id": "u%d", "title": "Ungrouped %d"}`, g, g))
	}
	src := `{"title": "Groups", "tasks": [` + strings.Join(tasks, ",") + `]}`
	for _, size := range [][2]int{{80, 24}, {80, 12}, {80, 10}, {44, 16}, {24, 7}, {120, 32}, {200, 60}} {
		a, _, _ := openDemo(t, src, size[0], size[1])
		keys := taskKeys(a.doc.plan)
		for i, key := range cursorKeys {
			press(a, key)
			if !slices.Contains(keys, a.outline.sel) {
				t.Fatalf("%v, key %d (%s): the cursor is on %q", size, i, key, a.outline.sel)
			}
			marks := 0
			for _, row := range strings.Split(planBody(a), "\n") {
				if strings.HasPrefix(row, "❯") {
					marks++
					if f := strings.Fields(strings.TrimLeft(row, "❯✔–! ")); len(f) == 0 || f[0] != a.outline.sel {
						t.Fatalf("%v, key %d (%s): ❯ on %q, not the task %q", size, i, key, row, a.outline.sel)
					}
				}
			}
			if marks != 1 {
				t.Fatalf("%v, key %d (%s): %d rows with ❯\n%s", size, i, key, marks, planBody(a))
			}
		}
	}
}

// docs/ux.md §4.2: enter (or →) on a task opens its details on a page of
// their own; on details, About, an empty plan and one Rex can't read it
// does nothing.
func TestEnterOnEveryRow(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 80, 24)
	for _, key := range []string{"\r", "right"} {
		press(a, "home", key)
		if a.at != atTaskDetail || a.outline.sel != "tab-bar" {
			t.Fatalf("%q on a task: at=%d sel=%q", key, a.at, a.outline.sel)
		}
		frame := a.frame()
		press(a, "\r", "right")
		if a.frame() != frame || a.at != atTaskDetail {
			t.Fatalf("enter and → on details do nothing\n%s", a.frame())
		}
		press(a, "esc")
	}
	press(a, "i")
	frame := a.frame()
	if press(a, "\r", "right"); a.at != atPlanDetail || a.frame() != frame {
		t.Fatalf("enter on About does nothing: at=%d", a.at)
	}
	for _, src := range []string{string(plan.Stub("demo")), `{"tasks": [`} {
		b, _, _ := openDemo(t, src, 80, 24)
		frame := b.frame()
		if press(b, "\r", "right", "down", "home"); b.at != atOutline || b.frame() != frame {
			t.Fatalf("%s: nothing to act on\n%s", src, b.frame())
		}
		if strings.Contains(frame, "enter") || strings.Contains(frame, "❯") {
			t.Fatalf("%s: no enter in the footer, no ❯\n%s", src, frame)
		}
	}
}

// docs/ux.md §4.3: Esc climbs one level and the footer names where:
// details and About to the list, on the same task; the list, an empty
// plan and one Rex can't read to the plan list. ← does the same.
func TestPlanEscLadder(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 80, 24)
	footer := func() string { r := a.frameRows().Rows; return textOf(r[len(r)-1], 80) }
	for _, back := range []string{"esc", "left"} {
		press(a, "down", "\r")
		if !strings.Contains(footer(), "esc tasks") || !strings.Contains(textOf(a.frameRows().Rows[0], 80), "Rex visual redesign › agent-stream") {
			t.Fatalf("details: %q", footer())
		}
		if press(a, back); a.at != atOutline || a.outline.sel != "agent-stream" {
			t.Fatalf("%s from details: at=%d sel=%q", back, a.at, a.outline.sel)
		}
		press(a, "i")
		if !strings.Contains(footer(), "esc tasks") || !strings.Contains(textOf(a.frameRows().Rows[0], 80), "Rex visual redesign › About") {
			t.Fatalf("About: %q", footer())
		}
		if press(a, back); a.at != atOutline || a.outline.sel != "agent-stream" {
			t.Fatalf("%s from About: at=%d sel=%q", back, a.at, a.outline.sel)
		}
		if !strings.Contains(footer(), "esc plans") {
			t.Fatalf("the list: %q", footer())
		}
		if press(a, back); a.at != atLibrary || a.plans.Rows[a.plans.Cursor].Kind != PlanRowContinue {
			t.Fatalf("%s from the list: Plans, on Continue: at=%d", back, a.at)
		}
		press(a, "\r", "up")
	}
	for _, src := range []string{string(plan.Stub("demo")), `{"tasks": [`} {
		b, _, _ := openDemo(t, src, 80, 24)
		if r := b.frameRows().Rows; !strings.Contains(textOf(r[23], 80), "esc plans") {
			t.Fatalf("%s: %q", src, textOf(r[23], 80))
		}
		if press(b, "esc"); b.at != atLibrary {
			t.Fatalf("%s: esc is the plan list: at=%d", src, b.at)
		}
	}
}

// docs/ux.md §1: a reload keeps the selection on the same task by id,
// wherever it moved in the file; a task that left, or became a parent,
// gives way to the pin.
func TestSelectionKeptByIDAcrossReloads(t *testing.T) {
	a, c, path := openDemo(t, specPlan, 80, 24)
	press(a, "down", "down", "down") // pin-card
	moved := strings.Replace(specPlan, ` {"id": "pin-card", "title": "Pinned card on the outline"},
`, "", 1)
	moved = strings.Replace(moved, `"tasks": [
`, `"tasks": [
 {"id": "new", "title": "New first"},
 {"id": "pin-card", "title": "Pinned card on the outline"},
`, 1)
	os.WriteFile(path, []byte(moved), 0o644)
	later(a, c)
	if a.outline.sel != "pin-card" || !strings.Contains(planBody(a), "❯   pin-card ") {
		t.Fatalf("moved to the top, the task keeps the cursor: %q\n%s", a.outline.sel, planBody(a))
	}
	parent := strings.Replace(moved, `{"id": "readme",`, `{"id": "kid", "title": "Kid", "parent": "pin-card"}, {"id": "readme",`, 1)
	os.WriteFile(path, []byte(parent), 0o644)
	later(a, c)
	if a.outline.sel != "page-titles" {
		t.Fatalf("a task that became a group gives the cursor to the pin: %q", a.outline.sel)
	}
	press(a, "end")
	os.WriteFile(path, []byte(strings.Replace(parent, `{"id": "readme", "title": "README screenshots", "status": "skipped"}`, `{"id": "other", "title": "Other"}`, 1)), 0o644)
	later(a, c)
	if a.outline.sel != "page-titles" {
		t.Fatalf("a task that left gives the cursor to the pin: %q", a.outline.sel)
	}
}

// A plan with no tasks has nothing to select; when the agent fills it,
// the cursor lands on its pin, as an open does (docs/ux.md §1).
func TestFilledPlanSelectsItsPin(t *testing.T) {
	a, c, path := openDemo(t, string(plan.Stub("demo")), 120, 32)
	if a.outline.sel != "" {
		t.Fatal("an empty plan has nothing to select")
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if a.outline.sel != "card-highlight" {
		t.Fatalf("when an agent fills the plan the cursor goes to the pin, got %q", a.outline.sel)
	}
}

// noteLabels are the labels of a pin note's field lines, in order: every
// line between the task line and the last.
func noteLabels(note string) []string {
	lines := strings.Split(note, "\n")
	var labels []string
	for _, l := range lines[2 : len(lines)-1] {
		label, _, _ := strings.Cut(l, ": ")
		labels = append(labels, label)
	}
	return labels
}

// pageField is one field of a page as it is drawn: its label, its
// values (one per row: no value wraps at the widths these tests draw),
// and whether they are muted.
type pageField struct {
	label  string
	values []string
	muted  bool
}

// pageFields are a page's fields, in order.
func pageFields(lines []LogLine) []pageField {
	var fields []pageField
	for _, l := range lines {
		if len(l.Spans) < 2 {
			continue
		}
		if label := strings.TrimSpace(l.Spans[0].Text); label != "" {
			fields = append(fields, pageField{label: label, muted: l.Spans[1].Role == "muted"})
		}
		if n := len(fields); n > 0 {
			fields[n-1].values = append(fields[n-1].values, l.Spans[1].Text)
		}
	}
	return fields
}

// pageLabels are a page's field labels, in order.
func pageLabels(lines []LogLine) []string {
	var labels []string
	for _, f := range pageFields(lines) {
		labels = append(labels, f.label)
	}
	return labels
}

var moreInFile = regexp.MustCompile(`^\(\+[0-9]+ more in the file\)$`)

// asNote is a page field worded as the pin note words its line: its
// values joined as the note joins them, then "(+N more in the file)",
// then Waits on's "(not done)".
func asNote(f pageField) string {
	values, more, tail := f.values, "", ""
	if n := len(values); n > 0 && moreInFile.MatchString(values[n-1]) {
		values, more = values[:n-1], " "+values[n-1]
	}
	if f.label == "Waits on" {
		var ids []string
		for _, v := range values {
			ids = append(ids, strings.TrimSuffix(v, " (not done)"))
		}
		values, tail = ids, " (not done)"
	}
	return f.label + ": " + strings.Join(values, plan.NoteField{Label: f.label}.Sep()) + more + tail
}

// docs/ux.md §6, §7a: a task's details are the fields of the note that
// pins it, in the note's order, as the note sends them: what isn't muted
// on the page is the note's field lines, word for word, values cut and
// lists trimmed as the note cuts them; what the note drops to stay under
// its 1,500 bytes is muted, named on a line of its own, and never in the
// note. For the CUJ fixture, a plan with every field, and one that runs
// past the cap.
func TestDetailsMirrorThePinNote(t *testing.T) {
	rich := `{"version": 1, "title": "Rich", "goal": "Everything", "base": "dev", "scope": {"out": ["plan out"]}, "tasks": [
 {"id": "base", "title": "Base", "status": "done", "branch": "feature/base", "accomplished": ["laid it"]},
 {"id": "dep", "title": "Dep"},
 {"id": "pin", "title": "Pin", "status": "active", "depends_on": ["base", "dep"], "does": ["d1", "d2"], "description": "desc", "needs": ["n1"], "verify": ["v1"], "files": ["a/b.go"], "scope": {"out": ["task out"]}, "branch": "feature/pin"},
 {"id": "other", "title": "Other", "status": "active"}]}`
	long := func(word string, n int) string { return strings.Repeat(word+" ", n/(len(word)+1)+1)[:n] }
	over, _ := json.Marshal(map[string]any{"version": 1, "title": "Over", "goal": long("goal", 180), "base": "dev", "scope": map[string]any{"out": []string{"web UI", "palettes"}},
		"tasks": []any{
			map[string]any{"id": "base", "title": "Base", "status": "done", "accomplished": []string{long("built", 120)}, "branch": "feature/base"},
			map[string]any{"id": "pin", "title": "The pinned task", "status": "active", "depends_on": []string{"base"}, "branch": "feature/pin",
				"does":        []string{long("does", 120), long("more", 120), long("also", 120)},
				"description": long("desc", 250), "needs": []string{long("need", 80)},
				"verify": []string{long("check", 200), long("test", 100), long("look", 100)},
				"files":  []string{"internal/tui/a.go", "internal/tui/b.go", "internal/tui/c.go", "internal/tui/d.go", "internal/tui/e.go", "internal/tui/f.go"}},
		}})
	const rel = "rex/plans/x.rex.json"
	for name, src := range map[string]string{"cuj": string(cujFixture(t)), "rich": rich, "over": string(over)} {
		p, err := plan.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		pin := p.Graph().Pin()
		note := plan.PinNote(&plan.Pinned{Rel: rel, Plan: p, Task: pin}, false)
		page := taskPage(&planDoc{plan: p}, rel, pin.ID, 400, "")
		lines := strings.Split(note, "\n")
		var sent, dropped []string
		for _, f := range pageFields(page) {
			if f.muted {
				dropped = append(dropped, f.label)
				continue
			}
			sent = append(sent, asNote(f))
		}
		if want := lines[2 : len(lines)-1]; !slices.Equal(sent, want) {
			t.Errorf("%s: what the page doesn't mute is the note\npage %q\nnote %q", name, sent, want)
		}
		all := pageLabels(page)
		if want := noteLabels(plan.PinNote(&plan.Pinned{Rel: rel, Plan: p, Task: pin}, false)); len(dropped) == 0 && !slices.Equal(all, want) {
			t.Errorf("%s: every field, in the note's order: %q, want %q", name, all, want)
		}
		notSent := ""
		for _, l := range page {
			if strings.HasPrefix(l.Text, "  Not sent (the note is capped at 1,500 bytes): ") {
				notSent = strings.TrimPrefix(l.Text, "  Not sent (the note is capped at 1,500 bytes): ")
			}
		}
		if notSent != strings.Join(dropped, ", ") {
			t.Errorf("%s: the page names what isn't sent: %q, muted %q", name, notSent, dropped)
		}
		if name == "over" && (len(note) > 1500 || len(dropped) == 0 || !strings.Contains(viewText(page), "more in the file)") || !strings.Contains(viewText(page), "…")) {
			t.Errorf("over: the note runs past its cap, so the page drops, trims and cuts (%d bytes)\n%s", len(note), viewText(page))
		}
	}
	p, _ := plan.Parse([]byte(rich))
	text := viewText(taskPage(&planDoc{plan: p}, rel, "pin", 100, ""))
	for _, want := range []string{"  Also active   other\n", "  Waits on      dep (not done)\n", "  Builds on     base (done): laid it\n", "  Out of scope  plan out\n                task out\n", "  Branch        feature/pin, stacked on feature/base\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("the rich pin's details have %q\n%s", want, text)
		}
	}
}

// docs/ux.md §6: a task's details (enter on it): row 0 is <plan> › <id>
// with its state, row 1 where it is; its title, a muted line saying
// whether its fields reach your agent, then the fields in a 14-cell label
// column. A done task leads with what your agent wrote when it finished
// it, then what was planned.
func TestTaskDetails(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 80, 24)
	rows := func() (string, string) { r := a.frameRows().Rows; return textOf(r[0], 80), textOf(r[1], 80) }
	press(a, "\r")
	if r0, r1 := rows(); r0 != sides("Rex visual redesign › page-titles", "pinned   plan · agent", 80) || r1 != sides("task 2 of 8", "not sent · ? help", 80) {
		t.Errorf("the pin's rows 0 and 1: %q / %q", r0, r1)
	}
	want := `  Page titles and footer keys
  Not sent: no wired agent takes the pin · ? help
  Does          Every screen opens with a title row that says where you are
                The footer lists keys only, in words
  Needs         the tab bar on row 0
  Verify        go test ./internal/tui -run PageTitle
                tmux at 80x24: every screen names itself
                NO_COLOR frame matches
  Files         internal/tui/chrome.go
                internal/tui/outline.go
                internal/tui/plans.go
  Builds on     tab-bar (done): Two tabs on row 0, the active one filled · Cue
                cell is fixed width
  Out of scope  extra palettes
                web UI
  Branch        feature/page_titles, stacked on feature/tab_bar
  Plan goal     A newcomer always knows which page they are on, what is pinned,
                and which key gets them where.`
	if got := trimmed(planBody(a)); got != want {
		t.Errorf("the pin's details:\n%s\nwant\n%s", got, want)
	}
	press(a, "esc", "down", "\r")
	if r0, r1 := rows(); r0 != sides("Rex visual redesign › agent-stream", "pending   plan · agent", 80) || r1 != "task 3 of 8" {
		t.Errorf("a pending task's rows 0 and 1: %q / %q", r0, r1)
	}
	if body := planBody(a); !strings.Contains(body, "  Pin it (p) to send these fields; no wired agent takes them now · ? help\n  Does          One row per run of thoughts on the Agent tab\n  Waits on      page-titles (not done)\n") {
		t.Errorf("a pending task's details\n%s", body)
	}
	press(a, "esc", "home", "\r")
	if r0, _ := rows(); r0 != sides("Rex visual redesign › tab-bar", "✔ done   plan · agent", 80) {
		t.Errorf("a done task's row 0: %q", r0)
	}
	want = `  Tab bar on row 0
  ✔ Done · what your agent wrote when it finished it
  Did           Two tabs on row 0, the active one filled
                Cue cell is fixed width
  Changed       internal/tui/chrome.go
                internal/tui/chrome_test.go
  Checked       go test ./internal/tui -run TabBar (passed)
                tmux at 80x24 and 120x32
  Notes         The cue cell must stay one cell wide; width_test pins it.
  Planned       Two places on row 0, the one on screen marked
  Branch        feature/tab_bar, stacked on dev`
	if got := trimmed(planBody(a)); got != want {
		t.Errorf("a done task's details:\n%s\nwant\n%s", got, want)
	}
	for _, line := range taskPage(a.doc, a.plans.CurrentRel(), "tab-bar", 80, "") {
		if strings.Contains(line.Text, "Planned") && line.Spans[1].Role != "muted" {
			t.Errorf("what was planned is muted: %+v", line)
		}
	}
	// Every state on row 0, and a task in a group on row 1.
	g, _, _ := openDemo(t, groupsPlan, 80, 24)
	pg := g.doc.plan
	for key, want := range map[string]string{"page-titles": "pinned · task 2 of 9 · in Chrome", "help-page": "blocked · task 6 of 9 · in Plan views", "readme": "skipped · task 9 of 9", "plan-list": "pending · task 4 of 9 · in Plan views › Plan list", "tab-bar": "✔ done · task 1 of 9 · in Chrome"} {
		word, _ := taskState(findTask(pg, key), pg.Graph(), pg.Graph().Pin())
		if got := word + " · " + taskPlace(pg, key); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
	// A done task with no notes yet; a read-only plan sends nothing; under
	// 50 body cells labels sit above their values.
	n, _, _ := openDemo(t, `{"version": 2, "tasks": [{"id": "a", "title": "A", "status": "done", "does": ["the work"]}, {"id": "b", "title": "B", "status": "active", "does": ["more work"]}]}`, 80, 24)
	if got := viewText(taskPage(n.doc, n.plans.CurrentRel(), "a", 80, "")); !strings.HasPrefix(got, "  A\n  ✔ Done · no notes yet\n  Planned       the work") {
		t.Errorf("done with no notes\n%s", got)
	}
	if got := viewText(taskPage(n.doc, n.plans.CurrentRel(), "b", 80, n.detailNote("b"))); !strings.Contains(got, "  A read-only plan sends your agent nothing.\n") {
		t.Errorf("read-only\n%s", got)
	}
	if got := viewText(taskPage(n.doc, n.plans.CurrentRel(), "b", 40, "")); !strings.Contains(got, "\n  Does\n    more work") {
		t.Errorf("40 cells: the label above its value\n%s", got)
	}
	// The line under the title wraps, as the title does, never cut.
	var intro []string
	for _, l := range taskPage(a.doc, a.plans.CurrentRel(), "agent-stream", 40, a.detailNote("agent-stream")) {
		if l.Text == "  Does" { // the first field's label, over its value under 50 cells
			break
		}
		if l.Kind == "muted" {
			intro = append(intro, strings.TrimSpace(l.Text))
			if cellLen(l.Text) > 39 {
				t.Errorf("40 cells: %q runs past the row", l.Text)
			}
		}
	}
	if got := strings.Join(intro, " "); got != "Pin it (p) to send these fields; no wired agent takes them now · ? help" {
		t.Errorf("40 cells: the line under the title, wrapped: %q", intro)
	}
}

// docs/ux.md §6: About is on i from the task list: the goal, scope,
// progress and stack, then problems and why the plan is read-only, in
// red. Enter does nothing there and Esc is the list.
func TestAboutOnI(t *testing.T) {
	a, _, _ := openDemo(t, specPlan, 80, 24)
	press(a, "down", "i")
	if a.at != atPlanDetail {
		t.Fatalf("i opens About: at=%d", a.at)
	}
	r := a.frameRows().Rows
	if got := textOf(r[0], 80); got != sides("Rex visual redesign › About", "active · 2/8 done   plan · agent", 80) || textOf(r[1], 80) != "rex/plans/cuj/demo.rex.json" {
		t.Errorf("About's rows 0 and 1: %q / %q", got, textOf(r[1], 80))
	}
	want := `  Goal          A newcomer always knows which page they are on, what is pinned,
                and which key gets them where.
  In scope      chrome · task list · plan list
  Out of scope  extra palettes · web UI
  Progress      2/8 done · 1 skipped · 1 blocked · 4 to go
  Stack         lands on dev · 3 branches`
	if got := trimmed(planBody(a)); got != want {
		t.Errorf("About:\n%s\nwant\n%s", got, want)
	}
	if press(a, "i", "esc"); a.at != atOutline || a.outline.sel != "agent-stream" {
		t.Fatalf("i does nothing on About, esc is the list on the same task: at=%d sel=%q", a.at, a.outline.sel)
	}
	press(a, "\r", "i")
	if a.at != atTaskDetail {
		t.Fatal("i does nothing on details")
	}
	b, _, _ := openDemo(t, `{"version": 2, "title": "Odd", "tasks": [{"id": "a", "depends_on": ["zz"]}]}`, 80, 24)
	press(b, "i")
	about := aboutPage(b.doc, 80)
	text := viewText(about)
	if !strings.Contains(text, "  Progress      0/1 done · 1 to go\n  Problems      plan: version 2 is newer than this Rex reads (1)\n                a: depends on \"zz\", which is not a task here\n  Read-only     version 2") {
		t.Errorf("problems and read-only come last\n%s", text)
	}
	for _, l := range about {
		if (strings.Contains(l.Text, "Problems") || strings.Contains(l.Text, "Read-only")) && l.Spans[1].Role != "error" {
			t.Errorf("in red: %+v", l)
		}
	}
	e, _, _ := openDemo(t, string(plan.Stub("demo")), 80, 24)
	if press(e, "i"); e.at != atPlanDetail || !strings.Contains(planBody(e), "  Progress      no tasks yet") {
		t.Errorf("an empty plan's About\n%s", planBody(e))
	}
}

// docs/ux.md §4, §6: opening a plan says which file opened on the notice
// row: picked, "Opened <title> · <path>"; written by your agent, "Your
// agent wrote <title> · opened <path>"; created, "✔ Created <path>".
// Narrow, the path stays and clips from the left.
func TestOpenedNotice(t *testing.T) {
	a := testApp(t)
	c := &clock{t: time.Unix(1_800_000_000, 0)}
	a.now = c.now
	path := writePlan(t, a.plans.Repo, "cuj/demo.rex.json", demoPlan)
	a.plans.walk = planWalk{}
	press(a, "esc")
	typeText(a, "cuj/demo")
	press(a, "\r")
	if got := a.notice(); got.Text != "Opened Send-off demo · rex/plans/cuj/demo.rex.json" || got.Kind != "text" || a.plans.Current != path {
		t.Fatalf("a pick: %q %q", got.Text, got.Kind)
	}
	for w, want := range map[int]string{38: "Opened rex/plans/cuj/demo.rex.json", 30: "Opened …lans/cuj/demo.rex.json"} {
		if got := a.notice().Fit(w); got != want || cellLen(got) > w {
			t.Errorf("%d cells: %q, want %q", w, got, want)
		}
	}
	c.t = c.t.Add(toastFor)
	if a.tick(); a.notice().Text != "" {
		t.Fatalf("it is a toast: %q", a.notice().Text)
	}
	press(a, "esc")
	typeText(a, "fresh")
	press(a, "\r")
	if got := a.notice().Text; got != "✔ Created rex/plans/fresh.rex.json" {
		t.Fatalf("a create: %q", got)
	}
	b := testApp(t)
	b.now = c.now
	other := writePlan(t, b.plans.Repo, "auth/reset.rex.json", strings.Replace(demoPlan, "Send-off demo", "Password reset", 1))
	b.ingest([]watch.Event{wrote(b.plans.Repo, other)})
	if got := b.notice().Text; got != "Your agent wrote Password reset · opened rex/plans/auth/reset.rex.json" {
		t.Fatalf("an attach: %q", got)
	}
	again := newApp("yellow", false, "idle", NewPlanBrowser(b.plans.Repo), 120, 32)
	if got := again.notice().Text; got != "" {
		t.Fatalf("start opens nothing, so says nothing: %q", got)
	}
	if press(again, "\r"); again.notice().Text != "Opened Password reset · rex/plans/auth/reset.rex.json" {
		t.Fatalf("Continue: %q", again.notice().Text)
	}
}

// docs/ux.md §4, §6: a list that goes on past its window says so: ↑ N more
// at row 2's right end from 12 rows, costing no row, or under 12 on a row
// of its own; ↓ N more on the list's last row. N counts the rows past it.
func TestListScrollMarkers(t *testing.T) {
	a, _, _ := openDemo(t, manyTasks(30), 80, 24)
	press(a, "end")
	rows := a.frameRows().Rows
	if got := textOf(rows[2], 80); got != strings.Repeat(" ", 70)+"↑ 11 more" {
		t.Errorf("row 2: %q", got)
	}
	if body := planBody(a); !strings.HasPrefix(body, "    t11 ") || !strings.Contains(body, "❯   t29 ") || strings.Contains(body, "more") {
		t.Errorf("the window ends on the last task\n%s", body)
	}
	press(a, "home")
	if got := textOf(a.frameRows().Rows[2], 80); got != "" || !strings.Contains(planBody(a), "\n  ↓ 12 more") {
		t.Errorf("at the top, only ↓: %q\n%s", got, planBody(a))
	}
	a.settle(80, 10)
	press(a, "end")
	if body := planBody(a); !strings.HasPrefix(body, "  ↑ 24 more\n") || !strings.HasSuffix(strings.TrimRight(body, "\n "), "❯   t29     Task number 29") {
		t.Errorf("under 12 rows ↑ takes a row\n%s", body)
	}
	press(a, "pageup")
	if body := planBody(a); !strings.HasPrefix(body, "  ↑ ") || !strings.Contains(body, "\n  ↓ ") {
		t.Errorf("both markers\n%s", body)
	}
}

// docs/ux.md §6: the list holds still as the cursor moves. While it fits,
// moving ↓ and ↑ over every task changes only which row has ❯; while it
// scrolls, the window moves only when the cursor would leave its margin.
func TestListHoldsStillAsTheCursorMoves(t *testing.T) {
	a, _, _ := openDemo(t, groupsPlan, 80, 24)
	blank := func() string { return strings.ReplaceAll(planBody(a), "❯", " ") }
	first := blank()
	for _, key := range cursorKeys {
		if press(a, key); blank() != first {
			t.Fatalf("%s moved the list\n%s\n--\n%s", key, first, blank())
		}
	}
	b, _, _ := openDemo(t, manyTasks(40), 80, 24)
	press(b, "home")
	top := planBody(b)
	for i := 0; i < 15; i++ {
		press(b, "down")
		if got := strings.ReplaceAll(planBody(b), "❯", " "); got != strings.ReplaceAll(top, "❯", " ") {
			t.Fatalf("down %d: the window moved before the cursor reached its margin\n%s", i+1, got)
		}
	}
	press(b, "down")
	if planBody(b) == top {
		t.Fatal("at the margin, the window moves")
	}
}

// docs/ux.md §3, §6: PgDn selects the task just past the window's last,
// PgUp the one just before its first, so no page skips a task the window
// didn't show; with nothing that way, the last or the first.
func TestTaskListPagesByItsWindow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {80, 10}, {44, 16}} {
		a, _, _ := openDemo(t, manyTasks(60), size[0], size[1])
		press(a, "home")
		seen := map[string]bool{}
		for i := 0; i < 20 && a.outline.sel != "t59"; i++ {
			for _, row := range strings.Split(planBody(a), "\n") {
				if f := strings.Fields(strings.TrimLeft(row, "❯✔ ")); len(f) > 0 && strings.HasPrefix(f[0], "t") {
					seen[f[0]] = true
				}
			}
			last := ""
			for _, row := range strings.Split(planBody(a), "\n") {
				if f := strings.Fields(strings.TrimLeft(row, "❯✔ ")); len(f) > 0 && strings.HasPrefix(f[0], "t") {
					last = f[0]
				}
			}
			press(a, "pagedown")
			if last == "t59" {
				break
			}
			if want := taskKeys(a.doc.plan)[slices.Index(taskKeys(a.doc.plan), last)+1]; a.outline.sel != want {
				t.Fatalf("%v: PgDn from a window ending %s landed on %s", size, last, a.outline.sel)
			}
		}
		if a.outline.sel != "t59" || len(seen) < 59 {
			t.Fatalf("%v: paging down reached %s and showed %d tasks", size, a.outline.sel, len(seen))
		}
		press(a, "pagedown")
		if a.outline.sel != "t59" {
			t.Fatal("PgDn at the end stays")
		}
		for i := 0; i < 20 && a.outline.sel != "t00"; i++ {
			press(a, "pageup")
		}
		if a.outline.sel != "t00" {
			t.Fatalf("%v: paging up reached %s", size, a.outline.sel)
		}
	}
}

func TestOutlineTabKeepsItsScreen(t *testing.T) {
	a, _, _ := openDemo(t, demoPlan, 120, 32)
	press(a, "down", "\r", "\t")
	if a.pane != PaneChat {
		t.Fatal("tab goes to the tape")
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "user", Text: "go"}})
	if row := textOf(a.frameRows().Rows[1], a.columns); !strings.HasSuffix(row, "pinned card-highlight") {
		t.Fatalf("AGENT's row 1 names the pin too (docs/ux.md §4): %q", row)
	}
	press(a, "\t")
	if a.at != atTaskDetail || a.outline.sel != "load-outline" {
		t.Fatal("tab back returns to the same details")
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
	settled(a, c)
	line := a.statusLine().Text
	if line != "The plan changed · load-outline → skipped · +1 task" {
		t.Fatalf("toast %q", line)
	}
	if a.outline.sel != "load-outline" {
		t.Fatal("the selection stays on the same task")
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "load-outline → skipped · +1 task" {
		t.Fatalf("tape line %+v", last)
	}
	if !strings.Contains(a.frame(), "– load-outline") || !strings.Contains(a.frame(), "extra") {
		t.Fatal("the list shows the new file")
	}
	c.t = c.t.Add(toastFor)
	a.tick()
	if a.statusLine().Text != "pinned  card-highlight · Selected plan card is one block" {
		t.Fatalf("the toast expires: %q", a.statusLine().Text)
	}
	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatal("reading the plan never writes it")
	}
}

// The agent pins and finishes tasks in the file, and Rex follows: row 1,
// the list and a toast (docs/ux.md §7, §8).
func TestTheAgentsPinAndDoneShow(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	done := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "depends_on": ["plans-ui"],
      "accomplished": ["the bar spans all three card rows"],
      "verified_how": ["go test ./internal/tui/"],
      "completed_notes": "the pick colors were already there",`, 1)
	os.WriteFile(path, []byte(done), 0o644)
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · next up load-outline" {
		t.Fatalf("the file finished the pin: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "card-highlight → done" {
		t.Fatalf("tape line %+v", last)
	}
	a.settle(38, 24) // 38 text cells: the done id clips, the next one stays
	if row := textOf(a.frameRows().Rows[22], 38); row != "✔ …hlight done · next up load-outline" {
		t.Fatalf("the notice row at 38×24: %q", row)
	}
	a.settle(120, 32)
	c.t = c.t.Add(toastFor)
	a.tick()
	if got := a.statusLine().Text; got != "nothing pinned · next up load-outline · Outline view" {
		t.Fatalf("then the next up task, with no verb to type: %q", got)
	}
	// The list marks it done and tags what is next up; the cursor stays.
	if body := planBody(a); !strings.Contains(body, "❯ ✔ card-highlight  Selected plan card is one block") || !strings.Contains(body, "    load-outline    Outline view") || !strings.HasSuffix(strings.Split(body, "\n")[2], "next up") {
		t.Fatalf("the list follows the file\n%s", body)
	}
	press(a, "\r")
	if a.at != atTaskDetail || a.outline.sel != "card-highlight" {
		t.Fatalf("at=%d sel=%q", a.at, a.outline.sel)
	}
	detail := viewText(taskPage(a.doc, a.plans.CurrentRel(), a.outline.sel, 78, ""))
	for _, want := range []string{"  ✔ Done · what your agent wrote when it finished it", "  Did           the bar spans all three card rows", "  Checked       go test ./internal/tui/", "  Notes         the pick colors were already there", "  Planned       Bar, border, and fill cover the whole selected card"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("the details show %q\n%s", want, detail)
		}
	}
	active := strings.Replace(done, `"status": "pending"`, `"status": "active"`, 1)
	os.WriteFile(path, []byte(active), 0o644)
	settled(a, c)
	if got := a.statusLine().Text; got != "The plan changed · load-outline → active" {
		t.Fatalf("the agent starts the next task: %q", got)
	}
	last := strings.Replace(active, `"status": "active",
      "depends_on": ["card-highlight"],`, `"status": "done",
      "depends_on": ["card-highlight"],`, 1)
	last = strings.Replace(last, `"status": "active",
  "scope"`, `"status": "done",
  "scope"`, 1)
	os.WriteFile(path, []byte(last), 0o644)
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ load-outline done · the plan is done" {
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
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · nothing else is unblocked" {
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
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · next up load-outline" {
		t.Fatalf("save 1: %q", got)
	}
	c.t = c.t.Add(time.Second)
	retro := strings.Replace(done, `"status": "done",
      "depends_on": ["plans-ui"],`, `"status": "done",
      "accomplished": ["the bar spans all three card rows"],
      "depends_on": ["plans-ui"],`, 1)
	os.WriteFile(path, []byte(retro), 0o644)
	settled(a, c) // 1.5 s after save 1
	if got := a.statusLine().Text; got != "✔ card-highlight done · next up load-outline" {
		t.Fatalf("save 2, the retrospective only: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "edited" {
		t.Fatalf("the tape still records it: %+v", last)
	}
	started := strings.Replace(retro, `"status": "pending"`, `"status": "active"`, 1)
	os.WriteFile(path, []byte(started), 0o644)
	settled(a, c)
	if got := a.statusLine().Text; got != "The plan changed · load-outline → active" {
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
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · now load-outline" {
		t.Fatalf("row 1 %q", got)
	}
	a.settle(38, 24) // 38 text cells: the done id clips, what is on now stays
	if row := textOf(a.frameRows().Rows[22], 38); row != "✔ …-highlight done · now load-outline" {
		t.Fatalf("the notice row at 38 cells: %q", row)
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
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · the plan is done" {
		t.Fatalf("every leaf is done or skipped, the plan's status not yet: %q", got)
	}
	whole := strings.Replace(leaves, `"status": "active",
  "scope"`, `"status": "done",
  "scope"`, 1)
	os.WriteFile(path, []byte(whole), 0o644)
	settled(a, c)
	if got := a.statusLine().Text; got != "✔ card-highlight done · the plan is done" {
		t.Fatalf("the plan's own status keeps the done news: %q", got)
	}
	if last := a.log[len(a.log)-1]; last.Kind != "plan" || last.Text != "plan → done" {
		t.Fatalf("tape line %+v", last)
	}
	c.t = c.t.Add(toastFor)
	a.tick()
	if got := a.statusLine().Text; got != "✔ all tasks done" {
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
		{44, "✔ card-highlight done · next up load-outline"},
		{38, "✔ …ghlight done · next up load-outline"},
		{34, "✔ …hlight done · next load-outline"},
		{20, "✔ done · next load-outline"}, // clip cuts the rest
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
	if got := a.statusLine().Text; !regexp.MustCompile(`^✗ plan JSON error \d+:\d+ · showing last good$`).MatchString(got) {
		t.Fatalf("status %q", got)
	}
	if !strings.Contains(a.frame(), "❯   card-highlight") || !strings.Contains(textOf(a.frameRows().Rows[0], 120), "1/3 done · can't read") {
		t.Fatalf("the last good plan stays on screen, and row 0 says it can't be read\n%s", a.frame())
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	if strings.Contains(a.statusLine().Text, "error") {
		t.Fatal("a fixed file clears the error")
	}
	os.Remove(path)
	later(a, c)
	if a.statusLine().Text != "✗ plan file missing · showing last good" {
		t.Fatalf("%q", a.statusLine().Text)
	}
}

// docs/ux.md §6, Plan states: a read-only plan says so in row 0's meta and
// on the notice row; a task in a loop and a second active task are tagged
// in red; row 1 says why nothing is pinned; a plan Rex can't read says why,
// and an empty one what happens next.
func TestPlanStates(t *testing.T) {
	a, _, _ := openDemo(t, `{"version": 2, "goal": "the goal", "tasks": [{"id": "a", "title": "A"}]}`, 120, 32)
	if rows := a.frameRows().Rows; textOf(rows[30], 120) != "Read-only: version 2 · newer than this Rex reads · p and d are off" || !strings.Contains(textOf(rows[0], 120), "0/1 done · read-only   plan · agent") || strings.Contains(planBody(a), "the goal") {
		t.Fatalf("%s", a.frame())
	}
	b, _, _ := openDemo(t, `{"tasks": [{"id": "a", "depends_on": ["zz"]}, {"id": "b", "depends_on": ["c"]}, {"id": "c", "depends_on": ["b"]}]}`, 80, 24)
	if body := trimmed(planBody(b)); b.statusLine().Text != "nothing pinned · nothing unblocked" || body != "❯   a       (untitled)\n    b       (untitled)"+strings.Repeat(" ", 52)+"cycle\n    c       (untitled)"+strings.Repeat(" ", 52)+"cycle" {
		t.Errorf("%q\n%q", b.statusLine().Text, body)
	}
	c, _, _ := openDemo(t, `{"tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "skipped"}]}`, 120, 32)
	if c.statusLine().Text != "✔ all tasks done" || !strings.Contains(planBody(c), "  – b ") {
		t.Fatalf("%q\n%s", c.statusLine().Text, c.frame())
	}
	d, _, _ := openDemo(t, `{"tasks": [`, 80, 24)
	if body := planBody(d); !strings.HasPrefix(body, "  Can't read this plan.\n  1:") || !strings.Contains(body, "  Rex shows it as soon as the file is valid JSON again.") || textOf(d.frameRows().Rows[0], 80) != sides("demo", "can't read   plan · agent", 80) || !strings.HasPrefix(d.statusLine().Text, "✗ plan JSON error 1:") || textOf(d.frameRows().Rows[1], 80) != "rex/plans/cuj/demo.rex.json" {
		t.Fatalf("%q\n%s", d.statusLine().Text, d.frame())
	}
	// Two active: the first is the pin, the second says so in red, at
	// every width.
	e, _, _ := openDemo(t, `{"tasks": [{"id": "a", "title": "A", "status": "active"}, {"id": "b", "title": "B", "status": "active"}]}`, 38, 24)
	if body := planBody(e); e.statusLine().Text != "pinned  a · A" || !strings.Contains(body, "❯   a"+strings.Repeat(" ", 26)+"pinned") || !strings.Contains(body, "    b"+strings.Repeat(" ", 21)+"also active") {
		t.Fatalf("%q\n%q", e.statusLine().Text, body)
	}
	e.settle(24, 24)
	if body := planBody(e); !strings.Contains(body, "    b       also active") {
		t.Errorf("24 cells keep also active\n%s", body)
	}
	// An active task with no id is never the pin, and the pin isn't
	// active: "also active" would point at nothing.
	n, _, _ := openDemo(t, `{"tasks": [{"title": "noid", "status": "active"}, {"id": "b", "title": "B"}]}`, 120, 24)
	if body := planBody(n); n.statusLine().Text != "nothing pinned · next up b · B" || !strings.Contains(body, "    #0      noid\n") || strings.Contains(body, "also active") {
		t.Errorf("%q\n%s", n.statusLine().Text, body)
	}
	if press(n, "home", "\r"); n.outline.sel != "#0" || textOf(n.frameRows().Rows[0], 120) != sides("demo › #0", "active   plan · agent", 78) {
		t.Errorf("details say active, not also active: %q", textOf(n.frameRows().Rows[0], 120))
	}
	// A parent is a group, never the pin.
	f, _, _ := openDemo(t, `{"tasks": [{"id": "ui", "title": "UI"}, {"id": "x", "parent": "ui", "status": "done"}, {"id": "y", "parent": "ui"}]}`, 120, 32)
	if body := planBody(f); !strings.HasPrefix(body, sides("  UI", "1/2", 78)+"\n") || f.outline.sel != "y" || !strings.Contains(body, "next up") {
		t.Fatalf("a parent is a group label with its rollup (sel %q)\n%q", f.outline.sel, body)
	}
	// An empty plan says what happens next, with nothing selected.
	g, _, _ := openDemo(t, string(plan.Stub("demo")), 80, 24)
	if body := trimmed(planBody(g)); body != "  No tasks yet.\n  Ask your agent to plan cuj/demo. It writes the tasks into\n  rex/plans/cuj/demo.rex.json, and they show up here as it saves." || g.outline.sel != "" {
		t.Fatalf("%s", body)
	}
}

// The task list has no composer: letters that aren't its keys (i, o, p,
// d), Backspace and a paste do nothing, ? opens Help at once, and Esc
// goes straight to the plan list (docs/ux.md §2, §3).
func TestOutlineKeysActAtOnce(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	before, _ := os.ReadFile(path)
	frame := a.frame()
	typeText(a, "zag sbx")
	press(a, "\x7f", "\x15", "\x17", PastePrefix+"pin load-outline")
	if a.frame() != frame || a.draft != "" {
		t.Fatalf("draft %q\n%s", a.draft, a.frame())
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("nothing typed on the task list writes the plan")
	}
	if press(a, "?"); a.overlay[PanePlans] != overlayHelp {
		t.Fatal("? opens Help at once")
	}
	if press(a, "i"); a.at != atOutline {
		t.Fatal("i does nothing under Help")
	}
	if press(a, "esc", "esc"); a.at != atLibrary {
		t.Fatalf("Esc closes Help, then goes up to the plan list: at=%d", a.at)
	}
}

// Start lands on Plans with Continue, the plan last opened in this repo,
// selected over a newer plan's Open latest; enter reopens it, following
// (docs/ux.md §11, §14), and Esc comes back to Continue.
func TestContinueAfterRestart(t *testing.T) {
	a, _, path := openDemo(t, demoPlan, 120, 32)
	// A newer plan doesn't win over the one in ~/.rex/state.json.
	other := filepath.Join(a.plans.Root, "zz-newer.rex.json")
	os.WriteFile(other, []byte(demoPlan), 0o644)
	stampAfter(t, other, path)
	again := newApp("yellow", false, "idle", NewPlanBrowser(a.plans.Repo), 120, 32)
	if row := again.plans.Rows[again.plans.Cursor]; again.at != atLibrary || row.Kind != PlanRowContinue || row.Path != path {
		t.Fatalf("at=%d on %+v", again.at, row)
	}
	if frame := again.frame(); !strings.Contains(frame, "❯ Continue        Send-off demo · 1/3 done · pinned card-highlight") || !strings.Contains(frame, "  Open latest     Send-off demo · 1/3 done · changed ") {
		t.Fatalf("%s", frame)
	}
	press(again, "\r")
	if again.at != atOutline || again.plans.Current != path || !again.follow || again.outline.sel != "card-highlight" {
		t.Fatalf("at=%d current=%q follow=%v sel=%q", again.at, again.plans.Current, again.follow, again.outline.sel)
	}
	if got := textOf(again.frameRows().Rows[0], 120); got != sides("Send-off demo", "1/3 done · following   plan · agent", 78) {
		t.Fatalf("row 0 names the plan and that Rex follows (docs/ux.md §4): %q", got)
	}
	if press(again, "esc"); again.plans.Rows[again.plans.Cursor].Kind != PlanRowContinue {
		t.Fatalf("cursor on %+v", again.plans.Rows[again.plans.Cursor])
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
	if a.outline.sel != "load-outline" || !strings.Contains(a.frame(), "This task is no longer in the file.") {
		t.Fatalf("the details say their task left instead of showing another\n%s", a.frame())
	}
	press(a, "esc")
	if a.outline.sel == "load-outline" {
		t.Fatal("back on the list, the selection falls back to a task that exists")
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

// The task list's body takes the rows a composer would have used: every row
// but rows 0 and 1, the footer, and from blankFrom rows the blank row 2
// and the notice row (docs/ux.md §4).
func TestOutlineHasNoComposer(t *testing.T) {
	for rows, chrome := range map[int]int{7: 3, 11: 3, 12: 5, 24: 5} {
		a, _, _ := openDemo(t, demoPlan, 120, rows)
		if f := a.frame(); strings.Contains(f, "│ > ") || strings.Contains(f, " > ") {
			t.Fatalf("%d rows: no composer\n%s", rows, f)
		}
		if got, want := a.layout().bodyH, rows-chrome; got != want {
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

// docs/ux.md §4: on row 0 the plan's title outranks its meta and the
// places' label, which go before the title clips; the cursor never moves
// it, and wider, the progress is back.
func TestNarrowOutlineTitleOutranksItsMeta(t *testing.T) {
	var tasks []string
	for i := 1; i <= 40; i++ {
		tasks = append(tasks, fmt.Sprintf(`{"id": "t%02d", "status": "done"}`, i))
	}
	tasks[39] = `{"id": "t40"}`
	src := `{"title": "A very long plan title that cannot fit", "status": "active", "tasks": [` + strings.Join(tasks, ",") + `]}`
	a, _, _ := openDemo(t, src, 38, 24)
	rows := a.frameRows().Rows
	if textOf(rows[0], 38) != sides("A very long plan title that c…", "plan", 38) || textOf(rows[1], 38) != "nothing pinned · next up t40" {
		t.Fatalf("row 0 is the title, cut beside the short label, row 1 the pin line\n%s", a.frame())
	}
	press(a, "home")
	if frame := a.frame(); textOf(a.frameRows().Rows[0], 38) != textOf(rows[0], 38) || !strings.Contains(frame, "❯ ✔ t01") {
		t.Fatalf("scrolled to the top, row 0 holds still\n%s", frame)
	}
	a.settle(120, 24)
	if got := textOf(a.frameRows().Rows[0], 120); got != sides("A very long plan title that cannot fit", "39/40 done   plan · agent", 78) {
		t.Fatalf("wider, the progress is back: %q", got)
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

// docs/ux.md §4: a long title on row 0 drops the meta and the label
// first, and a frame row never runs past the terminal.
func TestNarrowPageTitleClipsTheTitle(t *testing.T) {
	a, _, _ := openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "A very long plan title that cannot fit"`, 1), 38, 24)
	title := a.frameRows().Rows[0]
	if textOf(title, 38) != sides("A very long plan title that c…", "plan", 38) || cellLen(title) != 38 || !strings.Contains(a.frameRows().Rows[1], "Selected pl…") {
		t.Fatalf("%q", title)
	}
	// a tab is painted as spaces, so it is measured as spaces
	a, _, _ = openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Tabs\t\t\t\tin a long title"`, 1), 38, 24)
	if title := a.frameRows().Rows[0]; !strings.HasPrefix(title, "Tabs ") || cellLen(title) != 38 || strings.Contains(title, "\t") {
		t.Fatalf("%q", title)
	}
}

// docs/ux.md §6: About says where the stack lands; a task's details say
// where its PR goes, as the pin note does (docs/plan-format.md, Stacks).
func TestDetailShowsTheStackFields(t *testing.T) {
	p, err := plan.Parse([]byte(`{"base": "dev", "tasks": [{"id": "a", "branch": "feature/a"}, {"id": "b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &planDoc{path: "rex/plans/x.rex.json", plan: p}
	if got := trimmed(viewText(aboutPage(d, 60))); !strings.Contains(got, "\n  Stack         lands on dev · 1 branch") {
		t.Fatal(got)
	}
	if got := "\n" + trimmed(viewText(taskPage(d, "rex/plans/x.rex.json", "a", 60, ""))); !strings.Contains(got, "\n  Branch        feature/a, stacked on dev") {
		t.Fatal(got)
	}
	if got := "\n" + trimmed(viewText(taskPage(d, "rex/plans/x.rex.json", "b", 60, ""))); !strings.Contains(got, "\n  Branch        not named yet, stacked on dev") {
		t.Fatal(got)
	}
}

// docs/ux.md §6: a value keeps the file's line breaks, each line starting
// a row that hangs in the value column; but a field of the pin note shows
// as the note sends it, on one line, as the note folds it.
func TestDetailsKeepLineBreaks(t *testing.T) {
	p, err := plan.Parse([]byte(`{"title": "T", "goal": "Goal line one.\nGoal line two.", "tasks": [
		{"id": "a", "title": "A", "description": "para one.\n\n- item\n- item two", "needs": ["one\ntwo\nthree four five six seven eight nine ten eleven twelve"]},
		{"id": "b", "title": "B", "status": "done", "accomplished": ["did it"], "completed_notes": "note one.\n\nnote two."}]}`))
	if err != nil {
		t.Fatal(err)
	}
	d := &planDoc{path: "rex/plans/x.rex.json", plan: p}
	text := func(lines []LogLine) string { return trimmed(viewText(lines)) + "\n" }
	a := text(taskPage(d, "rex/plans/x.rex.json", "a", 60, ""))
	for _, want := range []string{"\n  Description   para one. - item - item two\n", "\n  Needs         one two three four five six seven eight\n                nine ten eleven twelve\n"} {
		if !strings.Contains(a, want) {
			t.Errorf("details have %q\n%s", want, a)
		}
	}
	if b := text(taskPage(d, "rex/plans/x.rex.json", "b", 60, "")); !strings.Contains(b, "\n  Notes         note one.\n                note two.\n") {
		t.Errorf("notes keep their paragraphs\n%s", b)
	}
	if about := text(aboutPage(d, 60)); !strings.HasPrefix(about, "  Goal          Goal line one.\n                Goal line two.\n") {
		t.Errorf("the goal keeps its lines\n%s", about)
	}
}

// docs/ux.md §6: a plan's text can be as long as a plan file (4 MB): the
// task list, task details and About lay out in time linear in it, never
// quadratic, so an agent that pastes a log into a description never
// freezes Rex.
func TestLongPlanTextLaysOutFast(t *testing.T) {
	long := strings.Repeat("word ", 2<<20/5)
	for _, field := range []string{"description", "does", "verify", "goal", "title"} {
		task := map[string]any{"id": "a", "title": "A", "status": "active"}
		doc := map[string]any{"title": "P", "tasks": []any{task, map[string]any{"id": "b", "title": "B"}}}
		switch field {
		case "goal":
			doc["goal"] = long
		case "description", "title":
			task[field] = long
		default:
			task[field] = []string{long}
		}
		src, _ := json.Marshal(doc)
		a, _, _ := openDemo(t, string(src), 80, 24)
		if a.doc.plan == nil {
			t.Fatalf("2 MB %s: %s", field, a.doc.err)
		}
		for _, screen := range [][]string{nil, {"\r"}, {"pagedown", "end"}, {"esc", "i"}, {"esc", "end"}} {
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

// docs/ux.md §6: a task list frame costs the rows it shows, not the text
// behind them, and the live loop draws one on every sprite change: 1200
// tasks with 600-byte titles and descriptions lay out and page in a few
// milliseconds here. A pin listing 50k files shows the note's first
// four; a done task listing them lays its details out once, and the
// frames that follow, and its scroll, cost only the rows they show.
// The bounds are loose enough for a slow CI runner.
func TestBigPlansLayOutFast(t *testing.T) {
	frame := func(a *app) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			start := time.Now()
			a.frameRows()
			best = min(best, time.Since(start))
		}
		return best
	}
	var files []string
	for i := 0; i < 50_000; i++ { // twice, in a plan under 4 MB
		files = append(files, fmt.Sprintf("internal/pkg%d/file_%d.go", i%50, i))
	}
	src, _ := json.Marshal(map[string]any{"title": "P", "tasks": []any{map[string]any{"id": "a", "title": "A", "status": "active", "files": files}, map[string]any{"id": "b", "title": "B", "status": "done", "files": files}}})
	a, _, _ := openDemo(t, string(src), 120, 40)
	if took, body := frame(a), planBody(a); took > 250*time.Millisecond || !strings.Contains(body, "❯   a ") {
		t.Errorf("a pin listing 50k files: a list frame takes %v\n%s", took, body)
	}
	press(a, "\r")
	if took, body := frame(a), planBody(a); took > 250*time.Millisecond || !strings.Contains(body, "                (+49996 more in the file)") {
		t.Errorf("its details, the note's first files: a frame takes %v\n%s", took, body)
	}
	// A done task's details list every file it planned.
	press(a, "esc", "down", "\r")
	start := time.Now()
	a.frameRows()
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("a done task's details lay out in %v", took)
	}
	press(a, "pagedown", "end")
	if took, body := frame(a), planBody(a); took > 250*time.Millisecond || !strings.Contains(body, "internal/pkg49/file_49999.go") || a.composeOptions().More == "" {
		t.Errorf("a frame of its details takes %v once laid out\n%s", took, body)
	}
	long := func(i, n int) string {
		w := fmt.Sprintf("word%d ", i)
		return strings.Repeat(w, n/len(w)+1)[:n]
	}
	var tasks []any
	for i := 0; i < 1200; i++ {
		tasks = append(tasks, map[string]any{"id": fmt.Sprintf("t%04d", i), "title": long(i, 600), "description": long(i, 600)})
	}
	src, _ = json.Marshal(map[string]any{"title": "Big", "goal": "a goal", "tasks": tasks})
	for _, size := range [][2]int{{200, 60}, {50, 24}} {
		a, _, _ := openDemo(t, string(src), size[0], size[1])
		press(a, "down")
		if took := frame(a); took > 500*time.Millisecond {
			t.Errorf("%v: 1200 long tasks, a frame takes %v", size, took)
		}
		start := time.Now()
		press(a, "pagedown")
		a.frameRows()
		if took := time.Since(start); took > time.Second || a.outline.sel == "t0001" {
			t.Errorf("%v: 1200 long tasks, PgDn to %s and its frame take %v", size, a.outline.sel, took)
		}
	}
}

// flatHead and flatJoin read only the head of a text, and cut it as cutting
// the whole flat text would: escapes, wide and joined glyphs, and a rune
// or a sequence the read cuts in two all come out the same.
func TestFlatHeadCutsAsTheWhole(t *testing.T) {
	for _, s := range []string{
		"", "plain words here and more", "wide 漢字漢字漢字 text", "ééé combining",
		"esc \x1b[31mred\x1b[0m and \x1b]8;;http://x/y\x07link\x1b]8;;\x07 done", "tab\there", "line\nbreak",
		"👍🏽 ok ⚠️ 1️⃣ 👨‍💻 end", "bad \xff utf8 \x07 bell", strings.Repeat("漢", 300),
	} {
		for _, cells := range []int{0, 1, 2, 3, 5, 8, 13, 40, 400} {
			if got, want := flatHead(s, cells), cutCells(flat(s), cells); got != want {
				t.Errorf("flatHead(%q, %d) = %q, want %q", s, cells, got, want)
			}
		}
	}
	items := []string{"one thing", "漢字 two", "three \x1b[1mbold\x1b[0m", "", "👍🏽 four"}
	for cells := 0; cells < 50; cells++ {
		if got, want := flatJoin(items, cells), cutCells(flat(strings.Join(items, " · ")), cells); got != want {
			t.Errorf("flatJoin(%d) = %q, want %q", cells, got, want)
		}
	}
}

// docs/ux.md §4, §6: the task list's body is its own, even when nothing
// of it fits: an empty read-only plan at 7 rows says what happens next,
// and the plan list never shows through under the plan's title.
// Read-only is the notice's, which takes row 1 under blankFrom rows.
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
			if !strings.HasPrefix(body, "  No tasks yet.") || strings.Contains(body, "+ New plan") || strings.Contains(body, "OPEN ─") {
				t.Errorf("%v: the task list's body, not the plan list's\n%s\nplan %s", size, a.frame(), src)
			}
			if p := a.doc.plan; p.ReadOnly != "" && !strings.HasPrefix(textOf(a.frameRows().Rows[1], size[0]), "Read-only: ") {
				t.Errorf("%v: a read-only plan says why on row 1\n%s", size, a.frame())
			}
		}
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

// docs/ux.md §12: as the terminal grows from 7 to 16 rows the task list
// never shows fewer tasks (nor, with groups, fewer rows). From 12 rows
// row 2 and the notice row take two rows an 11-row terminal gives the
// body, so 11 rows lay the list out as 12 do; a page too.
func TestTallerNeverShowsLessOfThePlan(t *testing.T) {
	count := func(a *app, task bool) int {
		keys := taskKeys(a.doc.plan)
		n := 0
		for _, line := range strings.Split(planBody(a), "\n") {
			f := strings.Fields(strings.TrimLeft(line, "❯✔–! "))
			switch {
			case len(f) == 0:
			case !task || slices.Contains(keys, f[0]):
				n++
			}
		}
		return n
	}
	for _, cols := range []int{38, 80, 120} {
		a, _, _ := cujApp(t)
		b, _, _ := cujApp(t)
		os.WriteFile(b.plans.Current, []byte(manyTasks(30)), 0o644)
		b.reloadDoc()
		wasTasks, wasFilled := -1, -1
		for rows := 7; rows <= 16; rows++ {
			a.settle(cols, rows)
			if n := count(a, true); n < wasTasks {
				t.Errorf("%d×%d shows %d tasks, fewer than %d at a row less\n%s", cols, rows, n, wasTasks, planBody(a))
			} else {
				wasTasks = n
			}
			b.settle(cols, rows)
			if n := count(b, false); n < wasFilled {
				t.Errorf("many %d×%d fills %d rows, fewer than %d at a row less\n%s", cols, rows, n, wasFilled, planBody(b))
			} else {
				wasFilled = n
			}
		}
	}
	a, _, _ := cujApp(t)
	press(a, "\r")
	a.settle(80, 12)
	twelve := planBody(a)
	a.settle(80, 11)
	if eleven := planBody(a); eleven != twelve+"\n" {
		t.Errorf("details at 11 rows show what 12 do, the last row blank:\n%s\n--\n%s", eleven, twelve)
	}
}

// docs/ux.md §6: a task in a parent loop has no top to sit under, so it
// is a row of its own at the top, tagged cycle in red, and never the pin;
// the plan has tasks, none done, and nothing pinned.
func TestParentLoopIsListed(t *testing.T) {
	a, _, _ := openDemo(t, `{"version":1,"title":"Loop","tasks":[{"id":"a","title":"A","parent":"b"},{"id":"b","title":"B","parent":"a"}]}`, 80, 24)
	r := a.frameRows().Rows
	if got := textOf(r[0], 80); got != sides("Loop", "0/2 done   plan · agent", 80) || textOf(r[1], 80) != sides("nothing pinned · nothing unblocked", "not sent", 80) {
		t.Fatalf("rows 0 and 1: %q / %q", got, textOf(r[1], 80))
	}
	want := "❯   a       A" + strings.Repeat(" ", 61) + "cycle\n    b       B" + strings.Repeat(" ", 61) + "cycle"
	if got := trimmed(planBody(a)); got != want {
		t.Fatalf("the loop's tasks are rows at the top\n%q\nwant\n%q", got, want)
	}
	if press(a, "\r"); textOf(a.frameRows().Rows[1], 80) != "task 1 of 2" {
		t.Errorf("in a loop, a task is in nothing: %q", textOf(a.frameRows().Rows[1], 80))
	}
	// A task under the loop sits inside it, and can be the pin.
	b, _, _ := openDemo(t, `{"version":1,"title":"Loop","tasks":[{"id":"a","title":"A","parent":"b"},{"id":"b","title":"B","parent":"a"},{"id":"c","title":"C","parent":"a"}]}`, 80, 24)
	if got := trimmed(planBody(b)); !strings.HasSuffix(got, "\n❯     c     C"+strings.Repeat(" ", 59)+"next up") || b.statusLine().Text != "nothing pinned · next up c · C" {
		t.Fatalf("%q\n%s", b.statusLine().Text, got)
	}
}

// docs/ux.md §4, §6: row 0's count is the done tasks of those the list
// shows: a skipped task isn't done, and a repeated id is one row. About's
// progress leads with the same count.
func TestProgressCountsTheListedTasks(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks":[{"id":"a","status":"done"},{"id":"a"},{"id":"b"},{"id":"c","status":"skipped"},{"id":"d"}]}`, 80, 24)
	rows := len(strings.Split(trimmed(planBody(a)), "\n"))
	if got := textOf(a.frameRows().Rows[0], 80); got != sides("demo", "1/4 done   plan · agent", 80) || rows != 4 {
		t.Fatalf("row 0 %q over %d rows", got, rows)
	}
	press(a, "i")
	if got := trimmed(planBody(a)); !strings.HasPrefix(got, "  Progress      1/4 done · 1 skipped · 2 to go\n") || !strings.Contains(textOf(a.frameRows().Rows[0], 80), "1/4 done") {
		t.Fatalf("About: %q", got)
	}
}

// docs/ux.md §6: a plan with no list (no tasks, or none Rex could read)
// is a page that scrolls on a short screen, with the list's counted
// markers; the footer says ↑↓ scroll only then.
func TestListlessPlansScroll(t *testing.T) {
	for _, src := range []string{string(plan.Stub("demo")), `{"tasks": [`} {
		a, _, _ := openDemo(t, src, 24, 7)
		footer := func() string { a.frameRows(); return spanText(segsOf(a.planFooter())) }
		body := planBody(a)
		if !strings.Contains(body, "\n  ↓ ") || !strings.HasPrefix(footer(), "↑↓ scroll") || strings.Contains(body, "↓ more") {
			t.Fatalf("%s: a counted ↓ and ↑↓ scroll\n%s\n%q", src, body, footer())
		}
		press(a, "down")
		if body := planBody(a); !strings.HasPrefix(body, "  ↑ 1 more\n") {
			t.Fatalf("%s: ↓ scrolls it\n%s", src, body)
		}
		press(a, "end")
		if body := strings.TrimRight(planBody(a), "\n "); strings.Contains(body, "↓") || !strings.HasSuffix(body, "again.") && !strings.HasSuffix(body, "saves.") {
			t.Fatalf("%s: end shows its last line\n%s", src, body)
		}
		if a.settle(80, 24); strings.Contains(footer(), "↑↓") {
			t.Fatalf("%s: a page that fits doesn't scroll: %q", src, footer())
		}
		if press(a, "esc"); a.at != atLibrary {
			t.Fatalf("%s: esc is the plan list", src)
		}
	}
}

// docs/ux.md §6: a deep chain of parents costs its length once per read of
// the plan, and a key on its list never walks it again.
func TestDeepChainKeysStayFast(t *testing.T) {
	var tasks []string
	for i := 0; i < 3000; i++ {
		parent := ""
		if i > 0 {
			parent = fmt.Sprintf(`, "parent": "p%d"`, i-1)
		}
		tasks = append(tasks, fmt.Sprintf(`{"id": "p%d", "title": "Part %d"%s}, {"id": "t%d", "title": "Task %d", "parent": "p%d"}`, i, i, parent, i, i, i))
	}
	a, _, _ := openDemo(t, `{"title": "Deep", "tasks": [`+strings.Join(tasks, ",")+`]}`, 80, 24)
	best := time.Hour
	for i := 0; i < 5; i++ {
		start := time.Now()
		press(a, "down")
		best = min(best, time.Since(start))
		a.frameRows()
	}
	if best > 50*time.Millisecond || a.outline.sel != "t5" {
		t.Fatalf("a ↓ on a 3,000-deep chain takes %v (sel %q)", best, a.outline.sel)
	}
	start := time.Now()
	a.frameRows()
	if took := time.Since(start); took > 250*time.Millisecond {
		t.Fatalf("a frame takes %v", took)
	}
}

// docs/ux.md §6: under 40 cells a group label keeps its progress, its
// title cut first.
func TestNarrowGroupLabelKeepsItsProgress(t *testing.T) {
	a, _, _ := openDemo(t, `{"tasks": [{"id": "g", "title": "A very long group title"}, {"id": "a", "parent": "g", "status": "done"}, {"id": "b", "parent": "g"}]}`, 24, 24)
	if body := planBody(a); !strings.HasPrefix(body, "  A very long gro…  1/2\n    ✔ a\n❯     b") {
		t.Fatalf("24 cells\n%s", body)
	}
	a.settle(38, 24)
	if body := planBody(a); !strings.HasPrefix(body, sides("  A very long group title", "1/2", 38)+"\n") {
		t.Fatalf("38 cells\n%s", body)
	}
}

// segsOf is a footer's segments as one line, unfitted.
func segsOf(segs []FooterSeg) []Span {
	var out []Span
	for i, s := range segs {
		if i > 0 {
			out = append(out, Span{Text: footerSep})
		}
		out = append(out, Span{Text: s.Text})
	}
	return out
}
