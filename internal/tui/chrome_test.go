package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

func TestLayoutTiers(t *testing.T) {
	cases := []struct {
		cols, lines            int
		mascot, text, composer int
		locked, typing         bool
	}{
		{120, 32, 40, 78, 3, false, true},
		{66, 24, 40, 24, 3, false, true},
		{65, 24, 0, 65, 3, false, true}, // text only
		{30, 24, 0, 30, 3, false, true},
		{23, 24, 0, 0, 3, true, false}, // widen
		{100, 15, 40, 58, 1, false, true},
		{100, 6, 40, 58, 0, false, false},
	}
	for _, c := range cases {
		l := layoutFor(c.cols, c.lines)
		if l.mascotCols != c.mascot || l.textW != c.text || l.composer != c.composer || l.locked != c.locked || l.typing != c.typing {
			t.Fatalf("%dx%d: %+v", c.cols, c.lines, l)
		}
	}
}

func frameAt(t *testing.T, cols, lines int, opt ComposeOptions) []string {
	t.Helper()
	opt.Columns, opt.Lines = cols, lines
	f := composeRows("idle", "yellow", opt)
	if len(f.Rows) != lines {
		t.Fatalf("%dx%d: %d rows", cols, lines, len(f.Rows))
	}
	for i, row := range f.Rows {
		if n := cellLen(row); n > cols {
			t.Fatalf("%dx%d row %d is %d cells: %q", cols, lines, i, n, row)
		}
	}
	return f.Rows
}

func TestTextOnlyBelow66AndWidenBelow24(t *testing.T) {
	rows := frameAt(t, 60, 24, ComposeOptions{Pane: PaneChat})
	if !strings.HasPrefix(rows[0], "REX PLAN [AGENT] idle") {
		t.Fatalf("text-only header should start at column 0: %q", rows[0])
	}
	if strings.ContainsAny(strings.Join(rows, ""), "▀▄█") {
		t.Fatal("mascot must be hidden below 66 cols")
	}
	narrow := frameAt(t, 20, 10, ComposeOptions{Pane: PaneChat})
	if !strings.HasPrefix(narrow[0], "widen to 24 cols") {
		t.Fatalf("%q", narrow[0])
	}
	if mid := frameAt(t, 23, 10, ComposeOptions{Pane: PaneChat}); !strings.HasPrefix(mid[0], "REX · widen to 24 cols") {
		t.Fatalf("%q", mid[0])
	}
	a := testApp(t)
	a.settle(20, 10)
	press(a, "\t", "x", "\r", "?")
	if a.pane != PanePlans || a.draft != "" || a.overlay[PanePlans] != overlayNone {
		t.Fatal("keys must not act on a UI that isn't visible, ? included")
	}
	press(a, CtrlC, CtrlC)
	if !a.quit {
		t.Fatal("ctrl+c still quits")
	}
}

func TestShortTerminalComposer(t *testing.T) {
	list := NewPlanBrowser(t.TempDir())
	rows := frameAt(t, 100, 12, ComposeOptions{Pane: PanePlans, Plans: list, Composer: true, Draft: "hi"})
	if !strings.Contains(rows[10], "/ hi") || strings.Contains(rows[10], "│") {
		t.Fatalf("under 16 rows the composer is one bare row: %q", rows[10])
	}
	if tape := frameAt(t, 100, 12, ComposeOptions{Pane: PaneChat, Draft: "hi"}); strings.Contains(strings.Join(tape, ""), "> hi") {
		t.Fatal("the tape has no composer at any height")
	}
	a := testApp(t)
	a.settle(100, 6)
	press(a, "esc", "x")
	if a.draft != "" || a.at != atLibrary {
		t.Fatal("under 7 rows printable keys are ignored")
	}
	if press(a, "?"); a.overlay[PanePlans] != overlayHelp {
		t.Fatal("? still opens Help: it types nothing")
	}
	// A draft typed before the terminal shrank is neither edited nor
	// submitted blind.
	b := testApp(t)
	press(b, "esc")
	typeText(b, "blind")
	b.settle(100, 6)
	press(b, "\x7f", "\r")
	if b.draft != "blind" || b.plans.Current != "" {
		t.Fatalf("draft %q current %q", b.draft, b.plans.Current)
	}
	press(b, "?")
	if b.overlay[PanePlans] != overlayNone || b.draft != "blind" {
		t.Fatal("a ? after text would be typed blind, so it is ignored too")
	}
	press(b, CtrlC, "x")
	if b.quitPending {
		t.Fatal("a swallowed key still disarms quit")
	}
	// The one-row composer leaves the cursor on screen.
	f := composeRows("idle", "yellow", ComposeOptions{Pane: PanePlans, Plans: list, Composer: true, Columns: 100, Lines: 12, Draft: strings.Repeat("x", 200)})
	if f.CursorCol > 100 {
		t.Fatalf("cursor at column %d of 100", f.CursorCol)
	}
}

func TestHomeFooterSaysCreateWhenThatIsWhatEnterDoes(t *testing.T) {
	repo := t.TempDir()
	seedPlans(t, repo, "alpha.rex.json")
	b := NewPlanBrowser(repo)
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			b.Cursor = i
		}
	}
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); segs[0].Text != "enter create" {
		t.Fatalf("on + New plan: %q", segs)
	}
	for i, row := range b.Rows {
		if row.Kind == PlanRowPlan {
			b.Cursor = i
		}
	}
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); segs[0].Text != "enter open" {
		t.Fatalf("on a plan: %q", segs)
	}
}

// docs/ux.md §12: under 7 rows the plan list's hidden composer takes no
// enter, so its footer offers none; the outline has no composer and enter
// still acts there, so its footer keeps it.
func TestShortTerminalFooterOffersNoEnter(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			b.Cursor = i
		}
	}
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); segs[0].Text != "enter create" {
		t.Fatalf("on Create the footer offers enter: %q", segs)
	}
	rows := frameAt(t, 100, 6, ComposeOptions{Pane: PanePlans, Plans: b, Composer: true})
	if strings.Contains(rows[len(rows)-1], "enter") {
		t.Fatalf("composer hidden, no enter: %q", rows[len(rows)-1])
	}

	a, _, _ := openDemo(t, demoPlan, 120, 32)
	a.settle(120, 6)
	rows = a.frameRows().Rows
	if footer := rows[len(rows)-1]; !strings.Contains(footer, "enter open") {
		t.Fatalf("the outline keeps enter open at 6 rows: %q", footer)
	}
	if press(a, "\r"); a.at != atTaskDetail {
		t.Fatalf("enter still opens the task: at=%d", a.at)
	}
}

// docs/ux.md §12: under 7 rows typing is ignored in the plan list, so its
// footer drops "type to find"; enter on an empty draft still opens the
// selected row, so "enter open" stays until a hidden draft would make
// enter a blind submit.
func TestShortPlanListFooterSaysWhatWorks(t *testing.T) {
	a, _, _ := cujApp(t)
	press(a, "esc")
	a.settle(50, 6)
	footer := func() string { rows := a.frameRows().Rows; return rows[len(rows)-1] }
	if f := footer(); strings.Contains(f, "type to") || !strings.Contains(f, "enter open") || !strings.Contains(f, "? help") {
		t.Fatalf("Home at 50×6: %q", f)
	}
	if typeText(a, "zz"); a.draft != "" || strings.Contains(footer(), "type to") {
		t.Fatalf("typing is ignored, and the footer doesn't offer it: %q %q", a.draft, footer())
	}
	if press(a, "\r"); a.at != atOutline {
		t.Fatalf("enter opens the open plan, as the footer says: at=%d", a.at)
	}

	press(a, "esc")
	a.settle(120, 32)
	typeText(a, "send")
	a.settle(50, 6)
	if f := footer(); strings.Contains(f, "enter") || strings.Contains(f, "? help") || !strings.Contains(f, "esc clear") {
		t.Fatalf("a hidden find: enter and ? would act on it blind: %q", f)
	}
	a.settle(120, 32)
	press(a, "esc", "home", "down", "\r") // the find cleared, then the cuj/ folder
	if a.plans.Screen != ScreenBrowse {
		t.Fatalf("browse: %d", a.plans.Screen)
	}
	a.settle(50, 6)
	if f := footer(); strings.Contains(f, "type to") || !strings.Contains(f, "enter open") {
		t.Fatalf("Browse at 50×6: %q", f)
	}
}

func TestExitLineIsCleaned(t *testing.T) {
	a := testApp(t)
	a.plans.Current = filepath.Join(a.plans.Root, "evil\x1b]52;c;eA==\x07.rex.json")
	if got := a.exitLine(); strings.ContainsAny(got, "\x1b\x07") || got != "rex: rex/plans/evil.rex.json" {
		t.Fatalf("%q", got)
	}
}

// plainRow is a chrome line as NO_COLOR paints it, trailing blanks off.
func plainRow(line LogLine, width int) string {
	return strings.TrimRight(paintLine(line, width, false), " ")
}

// textOf is the text column of a frame row at a terminal's width, trailing
// blanks off.
func textOf(row string, cols int) string {
	return strings.TrimRight(tailCells(stripSGR(row), PaneTextWidth(cols)), " ")
}

// docs/ux.md §4, row 0: the tab bar. Both places, the one on screen a
// pill, the cue between them in a 12-cell block so the turn word never
// moves; REX from 42 cells; the follow word and the path on the right.
func TestHeaderTabBar(t *testing.T) {
	const sd = "rex/plans/cuj/sendoff-demo.rex.json"
	type head struct {
		pane                     PaneKind
		cue, state, follow, path string
	}
	for _, tc := range []struct {
		width int
		heads []head
		want  []string
	}{
		{78, []head{{PanePlans, "•", "thinking", "following", sd}, {PaneChat, "•", "running", "latched", sd}, {PanePlans, "!", "ask", "following", sd}, {PanePlans, "", "idle", "", ""}}, []string{
			"REX [PLAN]•AGENT thinking        following rex/plans/cuj/sendoff-demo.rex.json",
			"REX PLAN•[AGENT] running           latched rex/plans/cuj/sendoff-demo.rex.json",
			"REX [PLAN]!AGENT ask             following rex/plans/cuj/sendoff-demo.rex.json",
			"REX [PLAN] AGENT idle",
		}},
		{38, []head{{PanePlans, "•", "thinking", "following", sd}, {PanePlans, "", "idle", "following", sd}, {PaneChat, "•", "running", "latched", "rex/plans/scratch.rex.json"}, {PanePlans, "!", "ask", "following", sd}, {PanePlans, "", "idle", "", ""}}, []string{
			"[PLAN]•AGENT thinking  following …demo",
			"[PLAN] AGENT idle  following …off-demo",
			"PLAN•[AGENT] running   latched scratch",
			"[PLAN]!AGENT ask  following …doff-demo",
			"[PLAN] AGENT idle",
		}},
		{24, []head{{PanePlans, "•", "thinking", "following", sd}, {PanePlans, "", "idle", "following", sd}, {PaneChat, "!", "running", "following", sd}}, []string{
			"[PLAN]•AGENT thinking",
			"[PLAN] AGENT idle  …demo",
			"PLAN![AGENT] running",
		}},
		{50, []head{{PanePlans, "•", "thinking", "following", sd}}, []string{"REX [PLAN]•AGENT thinking  following …sendoff-demo"}},
		{42, []head{{PanePlans, "•", "thinking", "following", sd}}, []string{"REX [PLAN]•AGENT thinking  following …demo"}},
		{41, []head{{PanePlans, "•", "thinking", "following", sd}}, []string{"[PLAN]•AGENT thinking  following …ff-demo"}},
	} {
		for i, h := range tc.heads {
			if got := plainRow(headerRow(tc.width, h.pane, h.cue, h.state, h.follow, h.path), tc.width); got != tc.want[i] {
				t.Errorf("%d cells:\n got %q\nwant %q", tc.width, got, tc.want[i])
			}
		}
	}
	// The turn word sits in the same column whatever the cue and the place.
	for _, w := range []int{24, 38, 41, 42, 78} {
		col := -1
		for _, pane := range []PaneKind{PanePlans, PaneChat} {
			for _, cue := range []string{"", "•", "!"} {
				row := plainRow(headerRow(w, pane, cue, "thinking", "following", sd), w)
				at := cellLen(row[:strings.Index(row, "thinking")])
				if col >= 0 && at != col {
					t.Errorf("%d cells, cue %q: the turn word moved from %d to %d", w, cue, col, at)
				}
				col = at
				if strings.HasPrefix(row, "REX ") != (w >= brandFrom) {
					t.Errorf("%d cells: REX %q", w, row)
				}
			}
		}
	}
	// At 38 cells with the longest turn word, the follow word stays and
	// the path keeps 5 cells or goes.
	for _, path := range []string{sd, "rex/plans/x.rex.json", "rex/plans/a/very/deep/and-long-plan-name.rex.json"} {
		row := plainRow(headerRow(38, PanePlans, "", "thinking", "following", path), 38)
		_, rest, ok := strings.Cut(row, " following")
		if rest = strings.TrimSpace(rest); !ok || (rest != "" && cellLen(rest) < min(headPathMin, cellLen(strings.TrimSuffix(strings.TrimPrefix(path, "rex/plans/"), ".rex.json")))) {
			t.Errorf("38 cells, %s: %q", path, row)
		}
	}
	// The pill has no fill (docs/ux.md §5): its brackets show in color
	// too, in the word's role, so they say where you are either way.
	var b strings.Builder
	for _, s := range headerRow(78, PanePlans, "", "idle", "", "").Spans {
		if s.Text == "[" || s.Text == "]" || s.Text == "PLAN" {
			b.WriteString(s.Role + " ")
		}
	}
	if got := b.String(); got != "text text text " {
		t.Errorf("the pill's roles: %q", got)
	}
}

func TestHeaderOnEveryScreen(t *testing.T) {
	b := NewPlanBrowser(t.TempDir())
	b.Current = b.Root + "/cuj/sendoff-demo.rex.json"
	for pane, want := range map[PaneKind]string{PanePlans: "REX [PLAN] AGENT thinking", PaneChat: "REX PLAN [AGENT] thinking"} {
		rows := frameAt(t, 120, 32, ComposeOptions{Pane: pane, Plans: b, Status: "thinking", PlanRel: b.CurrentRel(), Follow: "following"})
		if got := textOf(rows[0], 120); !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "following rex/plans/cuj/sendoff-demo.rex.json") {
			t.Fatalf("pane %d header %q", pane, got)
		}
	}
	narrow := frameAt(t, 80, 24, ComposeOptions{Pane: PaneChat, Status: "running", PlanRel: "rex/plans/cuj/sendoff-demo.rex.json"})
	if got := textOf(narrow[0], 80); !strings.HasSuffix(got, "running  cuj/sendoff-demo") {
		t.Fatalf("38-col header should drop rex/plans/ and .rex.json: %q", got)
	}
}

func TestShortPlanPath(t *testing.T) {
	path := "rex/plans/cuj/sendoff-demo.rex.json"
	for room, want := range map[int]string{
		40: path,
		30: "cuj/sendoff-demo.rex.json",
		20: "cuj/sendoff-demo",
		14: "…/sendoff-demo",
		10: "…doff-demo",
		5:  "…demo",
		4:  "",
	} {
		if got := shortPlanPath(path, room); got != want || cellLen(got) > room {
			t.Fatalf("room %d: %q want %q", room, got, want)
		}
	}
}

// docs/ux.md §4, row 1: progress on the right while a plan is open, "done"
// from 60 cells, dropped when the left would get under 12 cells. It never
// costs the line its words: it shows only where the line reads just as it
// does across the whole width, trying "1/3" when "1/3 done" doesn't fit;
// the pin line clips its title, and keeps only the id under 8 title cells.
// docs/ux.md §4, §5: row 1's news is text, never the pin's yellow: the
// done toast's ✔ is green and the rest text, and an offer is text; the
// agent's pin's mark is yellow and a computed one's is not.
func TestRowOneRoles(t *testing.T) {
	yellow, green, text := mascot.FG(mascot.UI["accent"]), mascot.FG(mascot.UI["action"]), mascot.FG(mascot.UI["text"])
	done, err := plan.Parse([]byte(`{"tasks": [{"id": "a", "status": "done"}, {"id": "b"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	toast := doneToast(&plan.Task{ID: "a"}, done)
	if row := statusRow(toast, 60, true); toast.Text != "✔ a done · next up b" || !strings.Contains(row, green+"✔") || !strings.Contains(row, text+" a done") || strings.Contains(row, yellow) {
		t.Errorf("the done toast: %q", row)
	}
	offer := StatusLine{Text: "new plan cuj/next · enter switch", Kind: "text", Spans: keySpans}
	if row := statusRow(offer, 60, true); strings.Contains(row, yellow) {
		t.Errorf("the offer: %q", row)
	}
	if row := statusRow(pinStatus(done), 60, true); !strings.HasPrefix(stripSGR(row), "▸ b (next up)") || strings.Contains(row, yellow) {
		t.Errorf("next up is not the pin's yellow: %q", row)
	}
	active, _ := plan.Parse([]byte(`{"tasks": [{"id": "a", "status": "active"}]}`))
	if row := statusRow(pinStatus(active), 60, true); !strings.Contains(row, yellow) {
		t.Errorf("the agent's pin is yellow: %q", row)
	}
}

func TestStatusProgress(t *testing.T) {
	p, err := plan.Parse([]byte(demoPlan))
	if err != nil {
		t.Fatal(err)
	}
	next, err := plan.Parse([]byte(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "done",
      "depends_on": ["plans-ui"]`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	offer := StatusLine{Text: "new plan cuj/next · enter switch", Kind: "text", Spans: keySpans, Fit: func(w int) string { return fitNewPlan("cuj/next", " · enter switch", "", w) }}
	toast := doneToast(&plan.Task{ID: "card-highlight"}, next)
	lines := []StatusLine{pinStatus(p), pinStatus(next), offer, toast,
		{Text: "drift · hard: README.md · outside card-highlight files", Short: "drift · hard: README.md", Kind: "accent"},
		{Text: "plan JSON error 14:3 · showing last good", Kind: "error"}}
	for i := range lines {
		lines[i].Right = "1/3"
	}
	lines[3].Right = "2/3"
	row := func(s StatusLine, w int) string { return strings.TrimRight(statusRow(s, w, false), " ") }
	for _, tc := range []struct {
		width int
		want  []string
	}{
		{78, []string{
			"▸ card-highlight  Selected plan card is one block                     1/3 done",
			"▸ load-outline  Outline view (next up)                                1/3 done",
			"new plan cuj/next · enter switch                                      1/3 done",
			"✔ card-highlight done · next up load-outline                          2/3 done",
			"drift · hard: README.md · outside card-highlight files                1/3 done",
			"plan JSON error 14:3 · showing last good                              1/3 done",
		}},
		{38, []string{
			"▸ card-highlight  Selected plan…   1/3",
			"▸ load-outline (next up)           1/3",
			"new plan cuj/next · enter switch   1/3",
			"✔ …ghlight done · next up load-outline",
			"drift · hard: README.md            1/3",
			"plan JSON error 14:3 · showing last g…",
		}},
		{24, []string{
			"▸ card-highlight     1/3",
			"▸ load-outline (next up)",
			"new plan cuj/next    1/3",
		}},
	} {
		for i, want := range tc.want {
			if got := row(lines[i], tc.width); got != want {
				t.Errorf("%d cells:\n got %q\nwant %q", tc.width, got, want)
			}
		}
	}
	// A line that fits the row whole keeps every word: "1/3" when "1/3
	// done" won't fit beside it, else no progress.
	stale := StatusLine{Text: skillHint(skill.StaleProject), Short: "rex-plan skill out of date · ? help", Spans: keySpans, Right: "2/30"}
	if got := row(stale, 78); !strings.HasPrefix(got, skillHint(skill.StaleProject)+" ") || !strings.HasSuffix(got, "  2/30") || cellLen(got) != 78 {
		t.Errorf("the command stays, with the short progress: %q", got)
	}
	long := StatusLine{Text: "▸ t1  " + strings.Repeat("x", 70), Right: "0/2"}
	if got := row(long, 78); got != long.Text {
		t.Errorf("no room beside a whole line: %q", got)
	}
	// A held attach keeps why it hasn't opened rather than make room.
	held := StatusLine{Text: "new plan two · opens when the line is clear", Right: "1/3", Fit: func(w int) string {
		return fitNewPlan("two", " · opens when the line is clear", " · waiting", w)
	}}
	if got := row(held, 24); got != "new plan two · waiting" {
		t.Errorf("24 cells: %q", got)
	}
	// "done" joins the progress from 60 cells.
	if got := row(lines[0], 60); !strings.HasSuffix(got, "  1/3 done") || cellLen(got) != 60 {
		t.Errorf("60 cells: %q", got)
	}
	if got := row(lines[0], 59); !strings.HasSuffix(got, "  1/3") || strings.HasSuffix(got, "done") || cellLen(got) != 59 {
		t.Errorf("59 cells: %q", got)
	}
	// The left keeps 12 cells: under that, progress goes.
	if got := row(StatusLine{Text: "▸ a", Right: "12/34"}, 18); got != "▸ a" {
		t.Errorf("no room for progress: %q", got)
	}
	if got := row(StatusLine{Text: "▸ a", Right: "12/34"}, 19); !strings.HasSuffix(got, "  12/34") {
		t.Errorf("room for progress: %q", got)
	}
	// Without a status there is nothing to show progress for.
	if got := row(StatusLine{Right: "1/3"}, 38); got != "" {
		t.Errorf("blank row 1: %q", got)
	}
	// Row 1 has no (active) any more: the agent's own pin needs no word.
	for _, s := range []StatusLine{pinStatus(p), pinStatus(next)} {
		if strings.Contains(s.Text, "(active)") {
			t.Errorf("%q", s.Text)
		}
	}
	// enter is drawn as a key, in bold; the agent's pin's mark is the pin's
	// yellow.
	painted := statusRow(offer, 78, true)
	if !strings.Contains(painted, "\033[1menter") {
		t.Errorf("enter is a key: %q", painted)
	}
	if spans := pinStatus(p).Spans("▸ card-highlight  Selected plan…"); len(spans) < 2 || spans[0].Text != "▸ card-highlight" || spans[0].Role != "accent" || !spans[0].Bold {
		t.Errorf("pin spans: %+v", spans)
	}
}

// docs/ux.md §4, row 2: the page title. Crumbs from column 2, the meta 1
// cell from the edge; narrow, the meta goes, then middle crumbs clip to 6
// cells, then the root becomes "…", and only the last crumb is cut.
func TestPageTitle(t *testing.T) {
	pages := []Page{
		{Crumbs: []string{"PLANS"}, Meta: "4 plans"},
		{Crumbs: []string{"PLANS", "find"}, Meta: "1 match"},
		{Crumbs: []string{"PLANS", "cuj/"}, Meta: "1 plan"},
		{Crumbs: []string{"PLANS", "new plan"}},
		{Crumbs: []string{"PLANS", "waiting for a plan"}},
		{Crumbs: []string{"PLANS", "Send-off demo"}, Meta: "active"},
		{Crumbs: []string{"PLANS", "Send-off demo", "about"}},
		{Crumbs: []string{"PLANS", "Send-off demo", "card-highlight"}},
		{Crumbs: []string{"AGENT"}, Meta: "live"},
		{Crumbs: []string{"AGENT"}, Meta: "12↑ · 5 new"},
		{Crumbs: []string{"PLANS", "Send-off demo", "HELP"}},
	}
	for _, tc := range []struct {
		width int
		want  []string
	}{
		{78, []string{
			"  PLANS                                                               4 plans",
			"  PLANS › find                                                        1 match",
			"  PLANS › cuj/                                                         1 plan",
			"  PLANS › new plan",
			"  PLANS › waiting for a plan",
			"  PLANS › Send-off demo                                                active",
			"  PLANS › Send-off demo › about",
			"  PLANS › Send-off demo › card-highlight",
			"  AGENT                                                                  live",
			"  AGENT                                                           12↑ · 5 new",
			"  PLANS › Send-off demo › HELP",
		}},
		{38, []string{
			"  PLANS                       4 plans",
			"  PLANS › find                1 match",
			"  PLANS › cuj/                 1 plan",
			"  PLANS › new plan",
			"  PLANS › waiting for a plan",
			"  PLANS › Send-off demo        active",
			"  PLANS › Send-off demo › about",
			"  PLANS › Send-off d… › card-highlight",
			"  AGENT                          live",
			"  AGENT                   12↑ · 5 new",
			"  PLANS › Send-off demo › HELP",
		}},
		{24, []string{
			"  PLANS         4 plans",
			"  PLANS › find  1 match",
			"  PLANS › cuj/   1 plan",
			"  PLANS › new plan",
			"  … › waiting for a plan",
			"  PLANS › Send-off demo",
			"  PLANS › Send-… › about",
			"  … › Send-… › card-hig…",
			"  AGENT            live",
			"  AGENT     12↑ · 5 new",
			"  PLANS › Send-o… › HELP",
		}},
	} {
		for i, p := range pages {
			line := pageLine(p, tc.width)
			if got := plainRow(line, tc.width); got != tc.want[i] {
				t.Errorf("%d cells:\n got %q\nwant %q", tc.width, got, tc.want[i])
			}
			// No bar (docs/ux.md §5): the current crumb is the bright bold
			// title, the crumbs before it and the meta muted.
			for j, sp := range line.Spans {
				switch {
				case j == 0:
				case sp.Text == p.Crumbs[len(p.Crumbs)-1] || strings.HasSuffix(sp.Text, "…") && sp.Bold:
					if sp.Role != "title" || !sp.Bold {
						t.Errorf("%d cells: the current crumb %+v", tc.width, sp)
					}
				case strings.TrimSpace(sp.Text) != "" && sp.Role != "muted":
					t.Errorf("%d cells: %q is %q, not muted", tc.width, sp.Text, sp.Role)
				}
			}
			// Only the last crumb is ever cut without a "…" of its own.
			got := plainRow(line, tc.width)
			for _, c := range p.Crumbs[:len(p.Crumbs)-1] {
				if !strings.Contains(got, c+" › ") && !strings.Contains(got, "… › ") {
					t.Errorf("%d cells: %q cut mid-crumb in %q", tc.width, c, got)
				}
			}
		}
	}
	// Selected, the title row is ❯ and the selection's purple, all of it.
	sel := pageLine(Page{Crumbs: []string{"PLANS", "Send-off demo"}, Meta: "active", Selected: true}, 38)
	if got := plainRow(sel, 38); got != "❯ PLANS › Send-off demo        active" || sel.Kind != "pick" {
		t.Errorf("the selected title row: %q kind %q", got, sel.Kind)
	}
	for _, sp := range sel.Spans {
		if sp.Role != "" {
			t.Errorf("the selected title row is all purple: %+v", sp)
		}
	}
	// The current crumb is bold; under NO_COLOR it is the same text, and
	// nothing is ever filled.
	painted := paintLine(pageLine(pages[5], 38), 38, true)
	if !strings.Contains(painted, "\033[1mSend-off demo") || stripSGR(painted) != paintLine(pageLine(pages[5], 38), 38, false) || strings.Contains(painted, "\033[48") {
		t.Errorf("%q", painted)
	}
}

// docs/ux.md §4 row 2 and §13, as the app builds them: each plan-list
// state's page title, its meta counting what the list holds, and the
// THIS PAGE of the Help it opens.
func TestPageTitleFromApp(t *testing.T) {
	repo := attachRepo(t)
	for _, name := range []string{"top.rex.json", "cuj/sendoff-demo.rex.json", "cuj/second.rex.json", "cuj/sub/deep.rex.json"} {
		writePlan(t, repo, name, demoPlan)
	}
	a := startIn(repo) // 120 columns: a 78-cell pane
	press(a, "esc")
	if a.at != atLibrary {
		t.Fatalf("not the plan list: at=%d", a.at)
	}
	row := func(left, meta string) string {
		if meta == "" {
			return left
		}
		return left + strings.Repeat(" ", 77-cellLen(left)-cellLen(meta)) + meta
	}
	to := func(kind PlanRowKind) {
		t.Helper()
		for i, r := range a.plans.Rows {
			if r.Kind == kind {
				for a.plans.Cursor < i {
					press(a, "down")
				}
				for a.plans.Cursor > i {
					press(a, "up")
				}
				return
			}
		}
		t.Fatalf("no row of kind %d in %+v", kind, a.plans.Rows)
	}
	check := func(step, left, meta, help string) {
		t.Helper()
		if got, want := textOf(a.frameRows().Rows[2], a.columns), row(left, meta); got != want {
			t.Fatalf("%s: row 2\n got %q\nwant %q", step, got, want)
		}
		if help == "" {
			return
		}
		press(a, "?")
		if got, want := textOf(a.frameRows().Rows[2], a.columns), left+" › HELP"; got != want {
			t.Fatalf("%s: Help's row 2\n got %q\nwant %q", step, got, want)
		}
		var h2 string
		for _, line := range a.helpBody(78) {
			if strings.HasPrefix(line.Text, "THIS PAGE ") {
				h2 = line.Text
			}
		}
		if !strings.HasSuffix(h2, "─ "+help) {
			t.Fatalf("%s: THIS PAGE is %q, want %q", step, h2, help)
		}
		press(a, "esc")
	}
	filter := func(step, text, left, meta string) {
		t.Helper()
		typeText(a, text)
		check(step+" "+text, left, meta, "")
		press(a, "\x15")
	}

	check("home", "  PLANS", "4 plans", "plans")
	filter("find", "se", "  PLANS › find", "2 matches")
	filter("find", "top", "  PLANS › find", "1 match")
	to(PlanRowNew)
	press(a, "\r")
	check("name from home", "  PLANS › new plan", "", "new plan")
	press(a, "esc")
	to(PlanRowMkdir)
	press(a, "\r")
	check("folder from home", "  PLANS › new folder", "", "new folder")
	press(a, "esc")

	// FOLDERS is Browse's root: a top folder's crumbs start under PLANS.
	to(PlanRowDir)
	press(a, "\r")
	check("browse", "  PLANS › cuj/", "3 plans", "folder")
	// A filter counts the plans under the rows it leaves: a folder counts
	// every plan in it and below.
	filter("browse", "s", "  PLANS › cuj/", "3 plans")
	filter("browse", "sub", "  PLANS › cuj/", "1 plan")
	filter("browse", "sec", "  PLANS › cuj/", "1 plan")
	filter("browse", "zzz", "  PLANS › cuj/", "0 plans")
	to(PlanRowMkdir)
	press(a, "\r")
	check("name from browse", "  PLANS › cuj/ › new folder", "", "new folder")
	press(a, "esc")

	to(PlanRowDir)
	press(a, "\r")
	check("deeper", "  PLANS › cuj/ › sub/", "1 plan", "folder")
	to(PlanRowNew)
	press(a, "\r")
	check("name from deeper", "  PLANS › cuj/ › sub/ › new plan", "", "new plan")
	press(a, "esc")

	press(a, "\t")
	check("agent", "  AGENT", "waiting for events", "agent")
	var many []watch.Event
	for i := 0; i < 40; i++ {
		many = append(many, watch.NewEvent("claude", "text", fmt.Sprintf("line %d", i), nil))
	}
	a.ingest(many)
	check("agent live", "  AGENT", "live", "")
	press(a, "up", "up")
	check("agent scrolled", "  AGENT", "2↑", "agent")
	a.ingest([]watch.Event{watch.NewEvent("claude", "text", "one more", nil)})
	if a.unseenLines == 0 || a.scroll != 2+a.unseenLines {
		t.Fatalf("scroll %d, %d new", a.scroll, a.unseenLines)
	}
	check("agent scrolled, news below", "  AGENT", fmt.Sprintf("%d↑ · %d new", a.scroll, a.unseenLines), "")
	// Scrolling down past the news: never more new lines than lie below.
	var more []watch.Event
	for i := 0; i < 5; i++ {
		more = append(more, watch.NewEvent("claude", "text", fmt.Sprintf("news %d", i), nil))
	}
	a.ingest(more)
	news := a.unseenLines
	for a.scroll > news-3 {
		press(a, "down")
	}
	if a.scroll == 0 || a.unseenLines != a.scroll {
		t.Fatalf("setup: scroll %d, %d new", a.scroll, a.unseenLines)
	}
	check("agent scrolled past the news", "  AGENT", fmt.Sprintf("%d↑ · %d new", a.scroll, a.scroll), "")
	press(a, "end")
	check("agent live again", "  AGENT", "live", "")
}

// docs/ux.md §13: Help keeps the page it was opened over. A plan the
// agent writes opens under it; THIS PAGE and the scroll stay put until Esc
// shows the outline, while the page title's crumbs follow the page under
// Help, so they still say where Esc goes (docs/ux.md §4).
func TestHelpKeepsThePageItOpenedOver(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	a.columns, a.lines = 80, 24
	press(a, "?")
	if a.at != atWait || a.overlay[PanePlans] != overlayHelp {
		t.Fatalf("at=%d overlay=%d", a.at, a.overlay[PanePlans])
	}
	press(a, "down", "down", "down")
	frame := func() (string, string) {
		rows := a.frameRows().Rows
		return textOf(rows[2], a.columns), strings.Join(rows[3:], "\n")
	}
	title, body := frame()
	if title != "  PLANS › waiting for a plan › HELP" || a.overlayScroll[PanePlans] != 3 || !strings.Contains(viewText(a.helpBody(38)), "─ waiting\n") {
		t.Fatalf("title %q scroll %d\n%s", title, a.overlayScroll[PanePlans], body)
	}
	a.ingest([]watch.Event{wrote(repo, writePlan(t, repo, "cuj/x.rex.json", demoPlan))})
	if a.at != atOutline {
		t.Fatalf("the plan opens under Help: at=%d", a.at)
	}
	if t2, b2 := frame(); t2 != "  PLANS › Send-off demo › HELP" || b2 != body || a.overlayScroll[PanePlans] != 3 {
		t.Fatalf("Help moved under the reader, or its crumbs didn't\n%s\n%s", t2, b2)
	}
	press(a, "esc")
	if title, _ := frame(); !strings.HasPrefix(title, "  PLANS › Send-off demo") {
		t.Fatalf("Esc shows the outline: %q", title)
	}
	press(a, "?")
	if title, _ := frame(); title != "  PLANS › Send-off demo › HELP" || !strings.Contains(viewText(a.helpBody(38)), "─ outline\n") {
		t.Fatalf("Help opens over the outline now: %q", title)
	}
	// Over a task, an attach under Help names the plan Esc lands on.
	press(a, "esc", "\r", "?")
	newer := writePlan(t, repo, "cuj/newer.rex.json", strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Brand new plan"`, 1))
	a.ingest([]watch.Event{wrote(repo, newer)})
	if title, _ := frame(); title != "  PLANS › Brand new plan › HELP" {
		t.Fatalf("Help's crumbs name the outline under it: %q", title)
	}
	if !strings.Contains(viewText(a.helpBody(38)), "─ task\n") {
		t.Fatal("THIS PAGE stays the task's")
	}
	press(a, "esc")
	if title, _ := frame(); !strings.HasPrefix(title, "  PLANS › Brand new plan") {
		t.Fatalf("Esc goes where the crumbs said: %q", title)
	}
	// AGENT under 12 rows has no page title of its own; Help's still says
	// where it is.
	a.lines = 11
	press(a, "\t", "?")
	if title, _ := frame(); title != "  AGENT › HELP" {
		t.Fatalf("Help over AGENT at 11 rows: %q", title)
	}
}

// docs/ux.md §5 (rule 3), §12: from 16 rows each H2 in Help has a blank
// row above it; on 15 none does.
func TestHelpGapsFrom16Rows(t *testing.T) {
	a := testApp(t)
	for _, lines := range []int{15, 16} {
		a.lines = lines
		press(a, "?")
		body := a.helpBody(38)
		h2s, gaps := 0, 0
		for i, line := range body {
			if len(line.Spans) > 1 && strings.HasPrefix(line.Spans[1].Text, " ───") {
				h2s++
				if i > 0 && body[i-1].Kind == "gap" {
					gaps++
				}
			}
		}
		want := 0
		if lines >= 16 {
			want = h2s
		}
		if h2s < 6 || gaps != want || (body[0].Kind == "gap") != (lines >= 16) {
			t.Fatalf("%d rows: %d of %d H2s have a gap above\n%s", lines, gaps, h2s, viewText(body))
		}
		press(a, "esc")
	}
}

// docs/ux.md §3, §4, §13: Help's THIS PAGE names every key its page acts
// on, not only the footer's. A key acts when it changes the frame, from
// where the page starts or after the opposite key, or what Esc shows next
// (so a Name prompt's Home and End must leave the list's cursor alone).
// enter that takes row 1's offer is said in AGENT's words.
func TestHelpListsEveryKeyThePageTakes(t *testing.T) {
	const switchWords = "switch PLAN to a new plan row 1 offers"
	pick := func(a *app, kind PlanRowKind) {
		for i, row := range a.plans.Rows {
			if row.Kind == kind {
				a.plans.Cursor = i
				return
			}
		}
		t.Fatalf("no row of kind %d", kind)
	}
	// A setup that writes a plan drops the cached walk (walkTTL), so the
	// list is the same whenever it is next built: on a slow runner the
	// cache can expire between the frames acts compares.
	offered := func(a *app) {
		path := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
		a.plans.walk = planWalk{}
		a.ingest([]watch.Event{wrote(a.plans.Repo, path)})
	}
	// Latched on the demo, as picking it in the plan list would, so enter
	// can still open a task.
	latched := func(a *app) { a.latch() }
	scrolls := func(a *app) {
		var evs []watch.Event
		for i := 0; i < 40; i++ {
			evs = append(evs, watch.NewEvent("claude", "text", fmt.Sprintf("reply %d", i), nil))
		}
		a.ingest(evs)
	}
	// Into rex/plans/cuj/, which holds a folder to go deeper into.
	browse := func(a *app) {
		writePlan(t, a.plans.Repo, "cuj/sub/deep.rex.json", demoPlan)
		a.plans.walk = planWalk{}
		press(a, "esc")
		a.plans.Screen, a.plans.Dir = ScreenBrowse, filepath.Join(a.plans.Root, "cuj")
		a.plans.Rebuild()
	}
	pages := []struct {
		page  string
		setup func(a *app)
	}{
		{"outline", func(a *app) {}},
		{"outline", func(a *app) { press(a, "home") }},
		{"outline", func(a *app) { latched(a); offered(a) }},
		{"about", func(a *app) { press(a, "home", "\r") }},
		{"about", func(a *app) { latched(a); press(a, "home", "\r"); offered(a) }},
		{"task", func(a *app) { latched(a); press(a, "\r"); offered(a) }},
		{"plans", func(a *app) { press(a, "esc") }},
		{"plans", func(a *app) { press(a, "esc"); typeText(a, "s") }},
		{"folder", browse},
		{"folder", func(a *app) { browse(a); pick(a, PlanRowDir); press(a, "\r") }},
		{"folder", func(a *app) { browse(a); typeText(a, "s") }},
		{"new plan", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r") }},
		{"new plan", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x") }},
		{"new folder", func(a *app) { browse(a); pick(a, PlanRowMkdir); press(a, "\r") }},
		{"waiting", nil},
		{"agent", func(a *app) { press(a, "\t"); scrolls(a) }},
		{"agent", func(a *app) { latched(a); press(a, "\t"); scrolls(a); offered(a) }},
	}
	words := map[string]string{"up": "↑↓", "down": "↑↓", "pageup": "pgup", "pagedown": "pgdn", "home": "home", "end": "end", "left": "←", "right": "→", "esc": "esc", "\r": "enter"}
	opposite := map[string]string{"up": "down", "down": "up", "pageup": "pagedown", "pagedown": "pageup", "home": "end", "end": "home"}
	for _, size := range [][2]int{{24, 7}, {80, 12}, {120, 32}} {
		for _, pg := range pages {
			start := func() *app {
				var a *app
				if pg.setup == nil {
					a = startIn(attachRepo(t))
				} else {
					a, _, _ = cujApp(t)
					pg.setup(a)
				}
				a.settle(size[0], size[1])
				if got := a.pageKind(); got != pg.page {
					t.Fatalf("%d×%d: setup for %q lands on %q", size[0], size[1], pg.page, got)
				}
				return a
			}
			first := start()
			var section []LogLine
			for _, line := range helpLines(78, pg.page, false)[1:] {
				if strings.HasPrefix(line.Text, "EVERYWHERE") {
					break
				}
				section = append(section, line)
			}
			listed := map[string]bool{}
			for _, line := range section {
				for _, s := range line.Spans {
					if s.Bold {
						for _, w := range strings.Fields(s.Text) {
							listed[w] = true
						}
					}
				}
			}
			text := viewText(section)
			for _, w := range strings.Fields(text) {
				if w == "pgup" || w == "pgdn" {
					listed[w] = true
				}
			}
			// Both key readers turn Ctrl+A and Ctrl+E into Home and End.
			if listed["home"] && (!strings.Contains(text, "ctrl+a") || !strings.Contains(text, "ctrl+e")) {
				t.Errorf("%d×%d %s: home and end, but not ctrl+a and ctrl+e\n%s", size[0], size[1], pg.page, text)
			}
			// acts says whether key changes the frame, or what Esc shows next.
			acts := func(before []string, key string) bool {
				a := start()
				press(a, before...)
				was := a.frame()
				if press(a, key); a.frame() != was {
					return true
				}
				b := start()
				press(b, before...)
				press(a, "esc")
				press(b, "esc")
				return a.frame() != b.frame()
			}
			if first.offerTakes() && !strings.Contains(strings.Join(strings.Fields(text), " "), switchWords) {
				t.Errorf("%d×%d %s: enter takes row 1's offer, but THIS PAGE doesn't say %q\n%s", size[0], size[1], pg.page, switchWords, text)
			}
			for key, word := range words {
				if listed[word] {
					continue
				}
				o, ok := opposite[key]
				if acts(nil, key) || ok && acts([]string{o}, key) {
					t.Errorf("%d×%d %s: %q acts, but THIS PAGE has no %q\n%s", size[0], size[1], pg.page, key, word, text)
				}
			}
		}
	}
}

// docs/ux.md §4: the longest prefix of segments that fits, joined by two
// spaces; the last one kept may shorten; rank is display order.
func TestFitFooter(t *testing.T) {
	segs := []FooterSeg{{Text: "esc plans"}, {Text: "tab agent"}, {Text: "enter open"}, helpSeg, {Text: "↑↓ move"}}
	for w, want := range map[int]string{
		78: "esc plans  tab agent  enter open  ? help  ↑↓ move",
		49: "esc plans  tab agent  enter open  ? help  ↑↓ move",
		48: "esc plans  tab agent  enter open  ? help",
		38: "esc plans  tab agent  enter open  ?",
		34: "esc plans  tab agent  enter open",
		24: "esc plans  tab agent",
		10: "esc plans",
		6:  "esc p…",
	} {
		if got := strings.Join(fitFooter(segs, w), "  "); got != want {
			t.Errorf("%d cells: %q want %q", w, got, want)
		}
	}
	if got := fitFooter(nil, 38); got != nil {
		t.Errorf("%q", got)
	}
	// Keys are bold, what they do is muted; the quit warning stays red.
	line := footerLine(segs, 78, "muted")
	if line.Spans[0].Text != "esc" || !line.Spans[0].Bold || line.Spans[1].Text != " plans" || line.Spans[1].Bold {
		t.Errorf("%+v", line.Spans)
	}
	quit := footerLine(segs[:0:0], 38, "error")
	if len(quit.Spans) != 0 {
		t.Errorf("%+v", quit.Spans)
	}
	quit = footerLine([]FooterSeg{{Text: "ctrl+c again to quit"}}, 38, "error")
	if quit.Spans[0].Text != "ctrl+c" || quit.Spans[0].Role != "error" || !quit.Spans[0].Bold {
		t.Errorf("%+v", quit.Spans)
	}
}

// docs/ux.md §4: each screen's footer at 38 cells (80×24) is exactly its
// must-haves: where Esc goes, Tab, what enter does and ? help, keys as
// words, two spaces apart. ? help is left out only where ? doesn't open
// Help (Help itself, a draft being typed, where ? is text) and on a detail
// screen while row 1 offers a plan, which has no room for it.
func TestFootersKeepMustHavesAt38Cells(t *testing.T) {
	footer := func(a *app) string {
		a.settle(a.columns, a.lines)
		rows := a.frameRows().Rows
		return textOf(rows[len(rows)-1], a.columns)
	}
	pick := func(a *app, kind PlanRowKind) {
		for i, row := range a.plans.Rows {
			if row.Kind == kind {
				a.plans.Cursor = i
			}
		}
	}
	offered := func(a *app) {
		other := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
		a.ingest([]watch.Event{wrote(a.plans.Repo, other)})
	}
	start := func() *app {
		a, _, _ := cujApp(t)
		writePlan(t, a.plans.Repo, "cuj/sub/deep.rex.json", demoPlan) // a folder to go deeper into
		a.settle(80, 24)
		return a
	}
	latched := func(a *app) { press(a, "esc"); typeText(a, "scratch"); press(a, "\r") }
	scrolled := func(a *app) {
		press(a, "\t")
		var evs []watch.Event
		for i := 0; i < 40; i++ {
			evs = append(evs, watch.NewEvent("claude", "text", fmt.Sprintf("reply %d", i), nil))
		}
		a.ingest(evs)
		press(a, "up")
	}
	for _, tc := range []struct {
		name  string
		setup func(a *app)
		want  string
	}{
		{"plan list", func(a *app) { press(a, "esc") }, "enter open  tab agent  ? help  ↑↓ move"},
		{"plan list on a folder", func(a *app) { press(a, "esc"); pick(a, PlanRowDir) }, "enter open  tab agent  ? help  ↑↓ move"},
		{"plan list on + New plan", func(a *app) { press(a, "esc"); pick(a, PlanRowNew) }, "enter create  tab agent  ? help"},
		{"plan list on + New folder", func(a *app) { press(a, "esc"); pick(a, PlanRowMkdir) }, "enter create  tab agent  ? help"},
		{"find", func(a *app) { press(a, "esc"); typeText(a, "send") }, "enter open  esc clear  tab agent"},
		{"find on + Create", func(a *app) { press(a, "esc"); typeText(a, "zz-new") }, "enter create  esc clear  tab agent"},
		{"browse, a top folder", func(a *app) { press(a, "esc"); pick(a, PlanRowDir); press(a, "\r") }, "enter open  esc plans  tab agent  ?"},
		{"browse, deeper", func(a *app) {
			press(a, "esc")
			pick(a, PlanRowDir)
			press(a, "\r")
			pick(a, PlanRowDir)
			press(a, "\r")
		}, "enter open  esc up  tab agent  ? help"},
		{"browse, a top folder, on + New folder", func(a *app) { press(a, "esc"); pick(a, PlanRowDir); press(a, "\r"); pick(a, PlanRowMkdir) }, "enter create  esc plans  tab agent  ?"},
		{"browse, a top folder, on + New plan", func(a *app) { press(a, "esc"); pick(a, PlanRowDir); press(a, "\r"); pick(a, PlanRowNew) }, "enter create  esc plans  tab agent  ?"},
		{"browse, deeper, on + New plan", func(a *app) {
			press(a, "esc")
			pick(a, PlanRowDir)
			press(a, "\r")
			pick(a, PlanRowDir)
			press(a, "\r")
			pick(a, PlanRowNew)
		}, "enter create  esc up  tab agent  ?"},
		{"plan list, /help typed", func(a *app) { press(a, "esc"); typeText(a, "/help") }, "enter help  esc clear  tab agent"},
		{"name prompt", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r") }, "enter create  esc cancel  tab agent  ?"},
		{"name prompt, named", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x") }, "enter create  esc clear  tab agent"},
		{"outline", func(a *app) {}, "esc plans  tab agent  enter open  ?"},
		{"outline, title row", func(a *app) { press(a, "home") }, "esc plans  tab agent  enter about  ?"},
		{"outline, offer", func(a *app) { press(a, "esc"); typeText(a, "scratch"); press(a, "\r"); offered(a) }, "enter switch  esc plans  tab agent  ?"},
		{"task detail", func(a *app) { press(a, "\r") }, "esc outline  tab agent  ↑↓ scroll  ?"},
		{"about", func(a *app) { press(a, "home", "\r") }, "esc outline  tab agent  ↑↓ scroll  ?"},
		{"detail, offer", func(a *app) { press(a, "esc"); typeText(a, "scratch"); press(a, "\r", "\r"); offered(a) }, "enter switch  esc outline  tab agent"},
		{"agent", func(a *app) { press(a, "\t"); a.ingest([]watch.Event{watch.NewEvent("claude", "text", "done", nil)}) }, "tab plan  ? help  ↑↓ scroll"},
		// The empty tab is a page: ↑↓ only while it runs past (docs/ux.md §10).
		{"agent, empty", func(a *app) { press(a, "\t") }, "tab plan  ? help  ctrl+c ×2 quit"},
		{"agent, empty, runs past", func(a *app) { a.health, a.hidden = notWired(), 4; press(a, "\t"); a.settle(80, 12) }, "tab plan  ? help  ↑↓ scroll"},
		{"agent, scrolled", scrolled, "end live  tab plan  ? help  ↑↓ scroll"},
		{"agent, scrolled, short", func(a *app) { scrolled(a); a.settle(80, 11) }, "1↑  end live  tab plan  ? help"},
		{"agent, offer", func(a *app) { press(a, "esc"); typeText(a, "scratch"); press(a, "\r", "\t"); offered(a) }, "enter switch  tab plan  ? help"},
		// With an offer, enter switch takes end live's place: ↑↓ still reach live.
		{"agent, scrolled + offer", func(a *app) { latched(a); scrolled(a); offered(a) }, "enter switch  tab plan  ? help"},
		{"agent, scrolled + offer, short", func(a *app) { latched(a); scrolled(a); offered(a); a.settle(80, 11) }, "1↑  enter switch  tab plan  ? help"},
		{"help on PLAN", func(a *app) { press(a, "?") }, "esc close  ↑↓ scroll  tab agent"},
		{"help on AGENT", func(a *app) { press(a, "\t", "?") }, "esc close  ↑↓ scroll  tab plan"},
		{"quit armed", func(a *app) { press(a, CtrlC) }, "ctrl+c again to quit"},
	} {
		a := start()
		tc.setup(a)
		if got := footer(a); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
	wait := testApp(t) // no plan yet: the wait screen (docs/ux.md §11)
	wait.settle(80, 24)
	if got := footer(wait); got != "esc plans  tab agent  ? help" {
		t.Errorf("wait: %q", got)
	}
	// With no plan open, Esc from the plan list goes back to Wait, and the
	// footer says so (docs/ux.md §3).
	empty := testApp(t)
	press(empty, "esc")
	empty.settle(80, 24)
	if got := footer(empty); got != "enter create  esc wait  tab agent  ?" {
		t.Errorf("plan list with no plans: %q", got)
	}
	unopened := testApp(t)
	writePlan(t, unopened.plans.Repo, "cuj/x.rex.json", demoPlan) // no hook, no poll yet
	unopened.plans.walk = planWalk{}
	press(unopened, "esc")
	unopened.settle(80, 24)
	if got := footer(unopened); unopened.plans.Current != "" || got != "enter open  esc wait  tab agent  ?" {
		t.Errorf("plan list with no plan open: %q", got)
	}
	// Under 12 rows a 4-digit scroll count leads AGENT's footer and ? help
	// still fits.
	deep := start()
	press(deep, "\t")
	var evs []watch.Event
	for i := 0; i < 400; i++ {
		evs = append(evs, watch.NewEvent("claude", "text", fmt.Sprintf("reply %d\n\nmore\n\nmore\n\nmore", i), nil))
	}
	deep.ingest(evs)
	deep.settle(80, 11)
	press(deep, "home")
	if got, want := footer(deep), fmt.Sprintf("%d↑  end live  tab plan  ? help", deep.scroll); deep.scroll < 1000 || got != want {
		t.Errorf("agent, 4-digit scroll, short:\n got %q\nwant %q", got, want)
	}
}

func TestStatusRowIsBlankWithoutStatus(t *testing.T) {
	rows := frameAt(t, 120, 32, ComposeOptions{Pane: PaneChat})
	if strings.TrimSpace(rows[1][42:]) != "" {
		t.Fatalf("row 1 %q", rows[1])
	}
	rows = frameAt(t, 120, 32, ComposeOptions{Pane: PaneChat, Line: StatusLine{Text: "plan updated", Kind: "muted"}})
	if !strings.Contains(rows[1], "plan updated") {
		t.Fatalf("row 1 %q", rows[1])
	}
}

func TestExitLine(t *testing.T) {
	a := testApp(t)
	if a.exitLine() != "rex: no plan open" {
		t.Fatal(a.exitLine())
	}
	a.plans.Current = a.plans.Root + "/alpha.rex.json"
	if a.exitLine() != "rex: rex/plans/alpha.rex.json" {
		t.Fatal(a.exitLine())
	}
}

// docs/ux.md §4, §5: an ask is bold text and a turn's error bold red;
// live work is text, never the pin's yellow.
func TestHeaderColorsAskAndError(t *testing.T) {
	for state, want := range map[string]string{"ask": "\033[38;2;226;232;240m\033[1mask", "error": "\033[38;2;232;78;78m\033[1merror", "thinking": "\033[38;2;226;232;240mthinking"} {
		opt := ComposeOptions{Enabled: true, Pane: PaneChat, Status: state, Log: []watch.Event{}}
		opt.Columns, opt.Lines = 120, 32
		if row := composeRows("idle", "yellow", opt).Rows[0]; !strings.Contains(row, want) {
			t.Errorf("%s: %q", state, row)
		}
	}
}

// docs/ux.md §5: H2 is a bold capitals label, a slate rule of at least
// three cells, and optional muted meta; H3 is a field in an 8-cell label
// column with its value hanging beside it.
func TestH2AndFields(t *testing.T) {
	var lines []LogLine
	lines = append(lines, h2Line("VERIFY", "2", 38), h2Line("CONTEXT", "", 38))
	lines = append(lines, fieldLines(h3{label: "needs", items: []string{"the pick and pickbg colors exist in mascot.UI"}}, 38, 8)...)
	lines = append(lines, fieldLines(h3{label: "does", items: []string{"one", "two"}}, 38, 8)...)
	lines = append(lines, fieldLines(h3{label: "files", items: []string{"internal/tui/plans.go", "internal/tui/live.go"}}, 38, 8)...)
	lines = append(lines, fieldLines(h3{label: "after", items: []string{"✔ plans-ui  Home cards, folder browser, duplicate names refused"}, bare: true, rows: 2}, 38, 8)...)
	lines = append(lines, fieldLines(h3{label: "changed", items: []string{"internal/tui/plans.go"}}, 38, h3Col(h3{label: "changed", items: []string{"x"}}))...)
	var got []string
	for _, l := range lines {
		got = append(got, plainRow(l, 38))
	}
	want := []string{
		"VERIFY ───────────────────────────── 2",
		"CONTEXT ──────────────────────────────",
		"  needs   the pick and pickbg colors",
		"          exist in mascot.UI",
		"  does    • one",
		"          • two",
		"  files   internal/tui/plans.go",
		"          internal/tui/live.go",
		"  after   ✔ plans-ui  Home cards,",
		"          folder browser, duplicate n…",
		"  changed  internal/tui/plans.go",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if h := h2Line("A LONG SECTION NAME", "a long meta", 24); plainRow(h, 24) != "A LONG SECTION NAME ────" {
		t.Fatalf("meta goes before the rule gets under 3 cells: %q", plainRow(h, 24))
	}
	if h := h2Line("FEED", "", 38); !h.Spans[0].Bold || h.Spans[1].Role != "border" {
		t.Fatalf("%+v", h.Spans)
	}
	if fieldLines(h3{label: "needs"}, 38, 8) != nil {
		t.Fatal("an empty field draws nothing")
	}
}
