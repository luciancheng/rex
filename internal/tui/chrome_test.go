package tui

import (
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

// docs/ux.md §4, §12: rows 0 and 1, the blank row 2 and the notice row
// from 12 rows, the footer; the text column is at most maxText cells.
func TestLayoutTiers(t *testing.T) {
	cases := []struct {
		cols, lines                   int
		mascot, text, composer, blank int
		locked, typing                bool
	}{
		{120, 32, 40, 78, 3, 1, false, true},
		{200, 32, 40, maxText, 3, 1, false, true}, // the measure
		{66, 24, 40, 24, 3, 1, false, true},
		{65, 24, 0, 65, 3, 1, false, true}, // text only
		{30, 24, 0, 30, 3, 1, false, true},
		{23, 24, 0, 0, 3, 1, true, false}, // widen
		{100, 15, 40, 58, 1, 1, false, true},
		{100, 12, 40, 58, 1, 1, false, true},
		{100, 11, 40, 58, 1, 0, false, true},
		{100, 6, 40, 58, 0, 0, false, false},
	}
	for _, c := range cases {
		l := layoutFor(c.cols, c.lines)
		if l.mascotCols != c.mascot || l.textW != c.text || l.composer != c.composer || l.blank != c.blank || l.notice != c.blank || l.locked != c.locked || l.typing != c.typing {
			t.Fatalf("%dx%d: %+v", c.cols, c.lines, l)
		}
		if want := max(0, c.lines-2-2*c.blank-FooterRows-c.composer); l.bodyH != want {
			t.Fatalf("%dx%d: body %d, want %d", c.cols, c.lines, l.bodyH, want)
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
	if !strings.HasPrefix(rows[0], "Agent ") || !strings.HasSuffix(strings.TrimRight(rows[0], " "), "plan · agent") {
		t.Fatalf("text-only, row 0 starts at column 0: %q", rows[0])
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

// docs/ux.md §4: the text column is used up to maxText cells; wider, the
// rest of each row stays empty, and the chrome ends a cell short of it.
func TestTextMeasure(t *testing.T) {
	a, _, _ := cujApp(t)
	a.settle(200, 40)
	f := a.frameRows()
	for i, row := range f.Rows {
		if n := cellLen(stripSGR(row)); n > 40+Gutter+maxText {
			t.Fatalf("row %d is %d cells past the measure: %q", i, n, stripSGR(row))
		}
	}
	if head := stripSGR(f.Rows[0]); !strings.HasSuffix(strings.TrimRight(head, " "), "plan · agent") || cellLen(strings.TrimRight(head, " ")) != 40+Gutter+maxText-1 {
		t.Fatalf("row 0 ends a cell short of the measure: %q", head)
	}
}

func TestShortTerminalComposer(t *testing.T) {
	list := NewPlanBrowser(t.TempDir())
	rows := frameAt(t, 100, 12, ComposeOptions{Pane: PanePlans, Plans: list, Composer: true, Draft: "hi"})
	if !strings.Contains(rows[9], "/ hi") || strings.Contains(rows[9], "│") {
		t.Fatalf("under 16 rows the composer is one bare row, over the notice row: %q", rows[9])
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
	// The one-row composer leaves the cursor on screen, on its row.
	f := composeRows("idle", "yellow", ComposeOptions{Pane: PanePlans, Plans: list, Composer: true, Columns: 100, Lines: 12, Draft: strings.Repeat("x", 200)})
	if f.CursorCol > 100 || f.CursorRow != 10 {
		t.Fatalf("cursor at row %d column %d of 100×12", f.CursorRow, f.CursorCol)
	}
	f = composeRows("idle", "yellow", ComposeOptions{Pane: PanePlans, Plans: list, Composer: true, Columns: 100, Lines: 24, Draft: "x"})
	if f.CursorRow != 21 {
		t.Fatalf("the boxed composer's cursor sits on its middle row, over the notice row: row %d", f.CursorRow)
	}
}

// docs/ux.md §4: what enter does on the selected row is the footer's
// enter, in words.
func TestHomeFooterSaysCreateWhenThatIsWhatEnterDoes(t *testing.T) {
	repo := t.TempDir()
	seedPlans(t, repo, "alpha.rex.json")
	b := NewPlanBrowser(repo)
	enter := func() string {
		for _, s := range footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0) {
			if strings.HasPrefix(s.Text, "enter ") {
				return s.Text
			}
		}
		return ""
	}
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			b.Cursor = i
		}
	}
	if got := enter(); got != "enter create" {
		t.Fatalf("on + New plan: %q", got)
	}
	for i, row := range b.Rows {
		if row.Kind == PlanRowPlan {
			b.Cursor = i
		}
	}
	if got := enter(); got != "enter open" {
		t.Fatalf("on a plan: %q", got)
	}
}

// docs/ux.md §12: under 7 rows the plan list's hidden composer takes no
// typing, so its footer offers none, and enter goes once a hidden draft
// would make it a blind submit; it doesn't depend on the row. The outline
// has no composer and enter still acts there, so its footer keeps it.
func TestShortTerminalFooterOffersNoEnter(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			b.Cursor = i
		}
	}
	rows := frameAt(t, 100, 6, ComposeOptions{Pane: PanePlans, Plans: b, Composer: true})
	if f := rows[len(rows)-1]; !strings.Contains(f, "enter create") || strings.Contains(f, "type to") {
		t.Fatalf("composer hidden, an empty draft: %q", f)
	}
	rows = frameAt(t, 100, 6, ComposeOptions{Pane: PanePlans, Plans: b, Composer: true, Draft: "x"})
	if f := rows[len(rows)-1]; strings.Contains(f, "enter") || strings.Contains(f, "? help") {
		t.Fatalf("composer hidden, a draft: %q", f)
	}
	// A Name prompt takes no typing there, so enter can't create: only its
	// way out and Help are offered.
	c := testApp(t)
	c.settle(80, 6)
	press(c, "esc", "\r")
	if !c.plans.Prompting() {
		t.Fatal("setup: the Name prompt")
	}
	if f := textOf(c.frameRows().Rows[5], 80); f != "esc cancel · tab agent · ? help" {
		t.Fatalf("a Name prompt under 7 rows: %q", f)
	}
	c.settle(80, 7)
	if f := textOf(c.frameRows().Rows[6], 80); !strings.HasPrefix(f, "enter create · ") {
		t.Fatalf("from 7 rows it takes a name, and enter creates: %q", f)
	}

	a, _, _ := openDemo(t, demoPlan, 120, 32)
	a.settle(120, 6)
	rows = a.frameRows().Rows
	if footer := rows[len(rows)-1]; !strings.Contains(footer, "enter details") {
		t.Fatalf("the outline keeps enter at 6 rows: %q", footer)
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

// sides is a row of width cells with left at its start and right ending a
// cell short of its edge, as row 0 and row 1 are drawn.
func sides(left, right string, width int) string {
	if right == "" {
		return left
	}
	return left + strings.Repeat(" ", width-1-cellLen(left)-cellLen(right)) + right
}

// docs/ux.md §4, row 0: the title row. The breadcrumb from column 0, its
// current crumb bright bold and the rest muted; the meta; the places'
// label three cells after it, ending a cell from the edge. Short of room
// the breadcrumb clips first, down to its current crumb; then the meta
// goes, its short form first; then the label shortens to the place on
// screen; under 30 cells there is none.
func TestTitleRow(t *testing.T) {
	pages := []Page{
		{Crumbs: []string{"Plans"}, Meta: metaSpans("rex · 4 plans"), Short: metaSpans("4 plans")},
		{Crumbs: []string{"Plans", "Find"}, Meta: metaSpans("1 match")},
		{Crumbs: []string{"Plans", "cuj/", "sub/"}, Meta: metaSpans("1 plan")},
		{Crumbs: []string{"Send-off demo"}, Meta: metaSpans("1/3 done · following"), Short: metaSpans("1/3")},
		{Crumbs: []string{"Send-off demo", "card-highlight"}, Meta: []Span{{Text: "pinned", Role: "accent"}}},
		{Crumbs: []string{"Send-off demo", "About"}, Meta: metaSpans("active · 1/3 done")},
		{Crumbs: []string{"Agent"}, Meta: metaSpans("live")},
		{Crumbs: []string{"Send-off demo", "card-highlight", "Help"}},
		{Crumbs: []string{"Plans", "Waiting for a plan"}, Meta: metaSpans("rex · no plans yet"), Short: metaSpans("no plans yet")},
	}
	const label = "plan · agent"
	for _, tc := range []struct {
		width int
		want  [][2]string
	}{
		{78, [][2]string{
			{"Plans", "rex · 4 plans   " + label},
			{"Plans › Find", "1 match   " + label},
			{"Plans › cuj/ › sub/", "1 plan   " + label},
			{"Send-off demo", "1/3 done · following   " + label},
			{"Send-off demo › card-highlight", "pinned   " + label},
			{"Send-off demo › About", "active · 1/3 done   " + label},
			{"Agent", "live   " + label},
			{"Send-off demo › card-highlight › Help", label},
			{"Plans › Waiting for a plan", "rex · no plans yet   " + label},
		}},
		{50, [][2]string{8: {"Waiting for a plan", "no plans yet   " + label}}},
		{40, [][2]string{8: {"… › Waiting for a plan", label}}},
		{38, [][2]string{
			{"Plans", "rex · 4 plans   " + label},
			{"Plans › Find", "1 match   " + label},
			{"… › sub/", "1 plan   " + label},
			{"Send-off demo", "1/3   " + label},
			{"card-highlight", "pinned   " + label},
			{"Send-off demo › About", label},
			{"Agent", "live   " + label},
			{"… › card-… › Help", label},
			{"… › Waiting for a plan", label},
		}},
		{33, [][2]string{8: {"… › Waiting for a plan", "plan"}}},
		{30, [][2]string{
			{"Plans", "4 plans   " + label},
			3: {"Send-off demo", label},
			6: {"Agent", "live   " + label},
			8: {"… › Waiting for a plan", "plan"},
		}},
		{29, [][2]string{
			{"Plans", "rex · 4 plans"},
			3: {"Send-off demo", "1/3"},
			8: {"Plans › Waiting for a plan", ""},
		}},
		{24, [][2]string{
			{"Plans", "rex · 4 plans"},
			3: {"Send-off demo", "1/3"},
			4: {"card-highlight", "pinned"},
			7: {"… › card-… › Help", ""},
			8: {"… › Waiting for a plan", ""},
		}},
	} {
		for i, want := range tc.want {
			if want == [2]string{} {
				continue
			}
			line := titleLine(pages[i], tc.width, PanePlans)
			if got, w := plainRow(line, tc.width), sides(want[0], want[1], tc.width); got != w {
				t.Errorf("%d cells, page %d:\n got %q\nwant %q", tc.width, i, got, w)
			}
		}
	}
	// The current crumb is the bright bold title, the crumbs before it, the
	// separators and the meta muted (unless a meta span says otherwise),
	// and the label's place on screen bright text and the rest muted.
	line := titleLine(pages[4], 78, PaneChat)
	roles := map[string]string{}
	for _, s := range line.Spans {
		if strings.TrimSpace(s.Text) != "" {
			roles[s.Text] = s.Role
			if s.Bold != (s.Text == "card-highlight") {
				t.Errorf("only the current crumb is bold: %+v", s)
			}
		}
	}
	for text, role := range map[string]string{"Send-off demo": "muted", " › ": "muted", "card-highlight": "title", "pinned": "accent", "plan": "muted", " · ": "muted", "agent": "text"} {
		if roles[text] != role {
			t.Errorf("%q is %q, want %q", text, roles[text], role)
		}
	}
	if short := placeLabel(PanePlans, true); spanText(short) != "plan" || short[0].Role != "text" {
		t.Errorf("the short label is the place on screen: %+v", short)
	}
	// Selected, the outline's title row is ❯ and the selection's purple,
	// but for the label, which keeps its roles.
	sel := titleLine(Page{Crumbs: []string{"Send-off demo"}, Meta: metaSpans("1/3 done"), Selected: true}, 50, PanePlans)
	if got := plainRow(sel, 50); got != sides("❯ Send-off demo", "1/3 done   "+label, 50) || sel.Kind != "pick" {
		t.Errorf("the selected title row: %q kind %q", got, sel.Kind)
	}
	for _, sp := range sel.Spans {
		switch sp.Text {
		case "plan", "agent", " · ":
			if sp.Role == "" {
				t.Errorf("the label keeps its roles on the selected row: %+v", sp)
			}
		default:
			if sp.Role != "" && strings.TrimSpace(sp.Text) != "" {
				t.Errorf("the selected title is all purple: %+v", sp)
			}
		}
	}
	// Under NO_COLOR it is the same text, and nothing is ever filled.
	painted := paintLine(line, 78, true)
	if !strings.Contains(painted, "\033[1mcard-highlight") || stripSGR(painted) != paintLine(line, 78, false) || strings.Contains(painted, "\033[48") {
		t.Errorf("%q", painted)
	}
}

// docs/ux.md §4, A1: the places' label never blinks or changes color with
// news; it names both places, the one on screen bright.
func TestPlaceLabel(t *testing.T) {
	a, _, _ := cujApp(t)
	label := func() []Span {
		l := titleLine(*a.composeOptions().Page, 78, a.pane)
		return l.Spans[len(l.Spans)-3:]
	}
	was := label()
	if spanText(was) != "plan · agent" || was[0].Role != "text" || was[2].Role != "muted" {
		t.Fatalf("on PLAN: %+v", was)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}, {Source: "claude", Kind: "error", Text: "the turn failed"}})
	if got := label(); fmt.Sprint(got) != fmt.Sprint(was) {
		t.Fatalf("news changed the label: %+v", got)
	}
	press(a, "\t")
	if got := label(); spanText(got) != "plan · agent" || got[0].Role != "muted" || got[2].Role != "text" {
		t.Fatalf("on AGENT: %+v", got)
	}
}

// docs/ux.md §4, row 1: the pin line. The agent's pin, its id in the
// pin's yellow and its title text; next up, never yellow; or why nothing
// is pinned. Narrow, the title clips, then the id stands alone.
func TestPinLine(t *testing.T) {
	parse := func(src string) *plan.Plan {
		p, err := plan.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	active := parse(demoPlan)
	next := parse(strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "pending",
      "depends_on": ["plans-ui"]`, 1))
	done := parse(`{"tasks": [{"id": "a", "status": "done"}, {"id": "b", "status": "skipped"}]}`)
	stuck := parse(`{"tasks": [{"id": "a", "status": "blocked"}]}`)
	empty := parse(`{"tasks": []}`)
	bare := parse(`{"tasks": [{"id": "a", "status": "active"}]}`)
	row := func(s StatusLine, w int) string { return strings.TrimRight(statusRow(s, w, false), " ") }
	for _, tc := range []struct {
		p     *plan.Plan
		width int
		want  string
	}{
		{active, 78, "pinned  card-highlight · Selected plan card is one block"},
		{active, 38, "pinned  card-highlight · Selected pl…"},
		{active, 30, "pinned  card-highlight"},
		{next, 78, "nothing pinned · next up card-highlight · Selected plan card is one block"},
		{next, 38, "nothing pinned · next up card-highli…"},
		{done, 38, "✔ all tasks done"},
		{stuck, 38, "nothing pinned · nothing unblocked"},
		{empty, 38, "nothing pinned · no tasks yet"},
		{bare, 38, "pinned  a"},
	} {
		if got := row(pinStatus(tc.p), tc.width); got != tc.want {
			t.Errorf("%d cells:\n got %q\nwant %q", tc.width, got, tc.want)
		}
	}
	yellow, text, green := mascot.FG(mascot.UI["accent"]), mascot.FG(mascot.UI["text"]), mascot.FG(mascot.UI["action"])
	if painted := statusRow(pinStatus(active), 78, true); !strings.Contains(painted, yellow+"card-highlight") || !strings.Contains(painted, text+"Selected plan card") {
		t.Errorf("the agent's pin is yellow, its title text: %q", painted)
	}
	if painted := statusRow(pinStatus(next), 78, true); strings.Contains(painted, yellow) || !strings.Contains(painted, text+"card-highlight") {
		t.Errorf("next up is never yellow: %q", painted)
	}
	if painted := statusRow(pinStatus(done), 38, true); !strings.Contains(painted, green+"✔") {
		t.Errorf("✔ is green: %q", painted)
	}
}

// docs/ux.md §4, the notice row: one message, the highest kind first: an
// error, your agent asking, a toast, an offer, hard drift, read-only. A
// toast lasts toastFor on the app's clock; the rest hold while they apply.
// Each is fitted by its own short form, and only an error is red.
func TestNoticePriorityAndExpiry(t *testing.T) {
	src := strings.Replace(driftDemo, `"title": "Drift demo",`, `"version": 2, "title": "Drift demo",`, 1)
	a, c, path := openDemo(t, src, 120, 32)
	kind := func() noticeKind { _, k := a.noticeOf(); return k }
	text := func() string { return a.notice().Text }
	if kind() != noticeReadOnly || !strings.HasPrefix(text(), "read-only: ") {
		t.Fatalf("a read-only plan: %v %q", kind(), text())
	}
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if kind() != noticeDrift || text() != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("hard drift outranks read-only: %v %q", kind(), text())
	}
	other := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, other)})
	if kind() != noticeOffer || text() != "Your agent wrote Send-off demo (cuj/next) · o opens it" {
		t.Fatalf("an offer outranks hard drift: %v %q", kind(), text())
	}
	os.WriteFile(path, []byte(strings.Replace(src, `"title": "Outline"`, `"title": "Outline, again"`, 1)), 0o644)
	later(a, c)
	if kind() != noticeToast || !strings.HasPrefix(text(), "plan updated") {
		t.Fatalf("a toast outranks the offer: %v %q", kind(), text())
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
	if kind() != noticeNeeds {
		t.Fatalf("your agent asking outranks a toast: %v %q", kind(), text())
	}
	os.WriteFile(path, []byte("{"), 0o644)
	later(a, c)
	later(a, c)
	if kind() != noticeError || !strings.HasPrefix(text(), "✗ plan JSON error") || a.notice().Kind != "error" {
		t.Fatalf("an error outranks everything: %v %q", kind(), text())
	}
	// Unwound: each holds while it applies, and the toast expires on the
	// clock, from when it was raised.
	os.WriteFile(path, []byte(src), 0o644)
	later(a, c)
	if kind() != noticeNeeds {
		t.Fatalf("the plan reads again: %v %q", kind(), text())
	}
	press(a, "\t", "\t") // a look at AGENT sees the ask, and the drift AGENT lists
	if kind() != noticeToast {
		t.Fatalf("back on PLAN: %v %q", kind(), text())
	}
	c.t = a.toastUntil.Add(-time.Millisecond)
	if a.tick(); kind() != noticeToast {
		t.Fatal("the toast holds until its time is up")
	}
	c.t = c.t.Add(time.Millisecond)
	if a.tick(); kind() != noticeOffer {
		t.Fatalf("then the offer is back: %v %q", kind(), text())
	}
	press(a, "o")
	if a.plans.Current != other || kind() != noticeNone {
		t.Fatalf("o takes the offer, and nothing else applies: %v %q", kind(), text())
	}
	// Narrow, each kind keeps what it is about.
	for _, s := range []StatusLine{
		errorNotice(StatusLine{Text: "plan JSON error 14:3 · showing last good", Kind: "error"}),
		{Text: "Your agent is asking you something · tab agent to read it", Short: "Your agent asks · tab agent"},
	} {
		if got := strings.TrimRight(statusRow(s, 30, false), " "); cellLen(got) > 29 || got == "" {
			t.Errorf("30 cells: %q", got)
		}
	}
}

// docs/ux.md §4: under 12 rows there is no notice row; a notice takes row
// 1 while it shows, so the footer always stays.
func TestNoticeTakesRowOneUnder12Rows(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 80, 12)
	pin := "pinned  card-highlight · Selected pl…"
	if r := a.frameRows().Rows; textOf(r[1], 80) != pin || textOf(r[10], 80) != "" {
		t.Fatalf("12 rows, nothing to say: row 1 %q, notice row %q", textOf(r[1], 80), textOf(r[10], 80))
	}
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"Outline view"`, `"Outline view, again"`, 1)), 0o644)
	later(a, c)
	if r := a.frameRows().Rows; textOf(r[1], 80) != pin || !strings.HasPrefix(textOf(r[10], 80), "plan updated") {
		t.Fatalf("12 rows: the notice row: %q", textOf(r[10], 80))
	}
	a.settle(80, 11)
	r := a.frameRows().Rows
	if !strings.HasPrefix(textOf(r[1], 80), "plan updated") || !strings.HasPrefix(textOf(r[10], 80), "enter details") {
		t.Fatalf("11 rows: the notice takes row 1, the footer stays: %q / %q", textOf(r[1], 80), textOf(r[10], 80))
	}
	c.t = c.t.Add(toastFor)
	if a.tick(); textOf(a.frameRows().Rows[1], 80) != pin {
		t.Fatalf("then row 1 is the pin line again: %q", textOf(a.frameRows().Rows[1], 80))
	}
}

// docs/ux.md §4 row 1: AGENT's is the turn, and on the right the pin the
// agent gets; an ask is text and a turn's error red.
func TestAgentRowOne(t *testing.T) {
	a, _, _ := cujApp(t)
	press(a, "\t")
	a.ingest([]watch.Event{{Source: "claude", Kind: "user", Text: "go"}})
	row := func() string { return textOf(a.frameRows().Rows[1], a.columns) }
	if got := row(); got != sides("thinking", "pinned card-highlight", 78) {
		t.Fatalf("thinking: %q", got)
	}
	a.enabled = true
	if painted := a.frameRows().Rows[1]; !strings.Contains(painted, mascot.FG(mascot.UI["accent"])+"card-highlight") {
		t.Fatalf("the pin's id is yellow: %q", painted)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
	if painted := a.frameRows().Rows[1]; !strings.Contains(painted, mascot.FG(mascot.UI["text"])+"waiting on you") {
		t.Fatalf("an ask is text: %q", painted)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "error", Text: "the turn failed"}})
	if painted := a.frameRows().Rows[1]; !strings.Contains(painted, mascot.FG(mascot.UI["error"])+"turn error") {
		t.Fatalf("a turn's error is red: %q", painted)
	}
	a.enabled = false
	a.settle(24, 12)
	if got := row(); got != "turn error" {
		t.Fatalf("narrow, the pin goes before the turn clips: %q", got)
	}
}

func TestStatusRowIsBlankWithoutStatus(t *testing.T) {
	rows := frameAt(t, 120, 32, ComposeOptions{Pane: PaneChat})
	if strings.TrimSpace(rows[1][42:]) != "" {
		t.Fatalf("row 1 %q", rows[1])
	}
	rows = frameAt(t, 120, 32, ComposeOptions{Pane: PaneChat, Context: StatusLine{Text: "plan updated", Kind: "muted"}})
	if !strings.Contains(rows[1], "plan updated") {
		t.Fatalf("row 1 %q", rows[1])
	}
}

// docs/ux.md §4, as the app builds them: each plan-list state's title,
// its meta counting what the list holds or saying where a name goes, and
// the THIS PAGE of the Help it opens.
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
	const label = "   plan · agent"
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
		right := strings.TrimPrefix(meta+label, "   ")
		if got, want := textOf(a.frameRows().Rows[0], a.columns), sides(left, right, 78); got != want {
			t.Fatalf("%s: row 0\n got %q\nwant %q", step, got, want)
		}
		if help == "" {
			return
		}
		press(a, "?")
		if got, want := textOf(a.frameRows().Rows[0], a.columns), sides(left+" › Help", "plan · agent", 78); got != want {
			t.Fatalf("%s: Help's row 0\n got %q\nwant %q", step, got, want)
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

	check("home", "Plans", filepath.Base(repo)+" · 4 plans", "plans")
	filter("find", "se", "Plans › Find", "2 matches")
	filter("find", "top", "Plans › Find", "1 match")
	to(PlanRowNew)
	press(a, "\r")
	check("name from home", "Plans › New plan", "rex/plans/", "new plan")
	press(a, "esc")
	to(PlanRowMkdir)
	press(a, "\r")
	check("folder from home", "Plans › New folder", "rex/plans/", "new folder")
	press(a, "esc")

	// FOLDERS is Browse's root: a top folder's crumbs start under Plans.
	to(PlanRowDir)
	press(a, "\r")
	check("browse", "Plans › cuj/", "3 plans", "folder")
	// A filter counts the plans under the rows it leaves: a folder counts
	// every plan in it and below.
	filter("browse", "s", "Plans › cuj/", "3 plans")
	filter("browse", "sub", "Plans › cuj/", "1 plan")
	filter("browse", "sec", "Plans › cuj/", "1 plan")
	filter("browse", "zzz", "Plans › cuj/", "0 plans")
	to(PlanRowMkdir)
	press(a, "\r")
	check("name from browse", "Plans › cuj/ › New folder", "rex/plans/cuj/", "new folder")
	press(a, "esc")

	to(PlanRowDir)
	press(a, "\r")
	check("deeper", "Plans › cuj/ › sub/", "1 plan", "folder")
	to(PlanRowNew)
	press(a, "\r")
	check("name from deeper", "Plans › cuj/ › sub/ › New plan", "rex/plans/cuj/sub/", "new plan")
	press(a, "esc")

	press(a, "\t")
	check("agent", "Agent", "no events yet", "agent")
	var many []watch.Event
	for i := 0; i < 40; i++ {
		many = append(many, watch.NewEvent("claude", "text", fmt.Sprintf("line %d", i), nil))
	}
	a.ingest(many)
	check("agent live", "Agent", "live", "")
	press(a, "up", "up")
	check("agent scrolled", "Agent", "↓ 2 newer", "agent")
	a.ingest([]watch.Event{watch.NewEvent("claude", "text", "one more", nil)})
	check("agent scrolled, news below", "Agent", fmt.Sprintf("↓ %d newer", a.scroll), "")
	press(a, "end")
	check("agent live again", "Agent", "live", "")
}

// docs/ux.md §13: Help keeps the page it was opened over. A plan the
// agent writes opens under it; THIS PAGE and the scroll stay put until Esc
// shows the outline, while the title's crumbs follow the page under Help,
// so they still say where Esc goes (docs/ux.md §4).
func TestHelpKeepsThePageItOpenedOver(t *testing.T) {
	repo := attachRepo(t)
	a := startIn(repo)
	a.columns, a.lines = 120, 24
	press(a, "?")
	if a.at != atWait || a.overlay[PanePlans] != overlayHelp {
		t.Fatalf("at=%d overlay=%d", a.at, a.overlay[PanePlans])
	}
	press(a, "down", "down", "down")
	frame := func() (string, string) {
		rows := a.frameRows().Rows
		return textOf(rows[0], a.columns), strings.Join(rows[3:len(rows)-2], "\n") // not the notice row: an attach's toast
	}
	crumbs := func(title string) string {
		crumbs, _, _ := strings.Cut(title, "   ")
		return strings.TrimSpace(crumbs)
	}
	title, body := frame()
	if crumbs(title) != "Plans › Waiting for a plan › Help" || a.overlayScroll[PanePlans] != 3 || !strings.Contains(viewText(a.helpBody(38)), "─ waiting\n") {
		t.Fatalf("title %q scroll %d\n%s", title, a.overlayScroll[PanePlans], body)
	}
	a.ingest([]watch.Event{wrote(repo, writePlan(t, repo, "cuj/x.rex.json", demoPlan))})
	if a.at != atOutline {
		t.Fatalf("the plan opens under Help: at=%d", a.at)
	}
	if t2, b2 := frame(); crumbs(t2) != "Send-off demo › Help" || b2 != body || a.overlayScroll[PanePlans] != 3 {
		t.Fatalf("Help moved under the reader, or its crumbs didn't\n%s\n%s", t2, b2)
	}
	press(a, "esc")
	if title, _ := frame(); !strings.HasPrefix(title, "  Send-off demo ") {
		t.Fatalf("Esc shows the outline: %q", title)
	}
	press(a, "?")
	if title, _ := frame(); crumbs(title) != "Send-off demo › Help" || !strings.Contains(viewText(a.helpBody(38)), "─ outline\n") {
		t.Fatalf("Help opens over the outline now: %q", title)
	}
	// Over a task, an attach under Help names the plan Esc lands on.
	press(a, "esc", "\r", "?")
	newer := writePlan(t, repo, "cuj/newer.rex.json", strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Brand new plan"`, 1))
	a.ingest([]watch.Event{wrote(repo, newer)})
	if title, _ := frame(); crumbs(title) != "Brand new plan › Help" {
		t.Fatalf("Help's crumbs name the outline under it: %q", title)
	}
	if !strings.Contains(viewText(a.helpBody(38)), "─ task\n") {
		t.Fatal("THIS PAGE stays the task's")
	}
	press(a, "esc")
	if title, _ := frame(); !strings.HasPrefix(title, "  Brand new plan ") {
		t.Fatalf("Esc goes where the crumbs said: %q", title)
	}
	// AGENT has a title at every height, and so does Help over it.
	a.lines = 11
	press(a, "\t", "?")
	if title, _ := frame(); crumbs(title) != "Agent › Help" {
		t.Fatalf("Help over AGENT at 11 rows: %q", title)
	}
}

// docs/ux.md §5 (rule 3), §12: from 16 rows each H2 in Help but the first
// has a blank row above it (the first's is row 2); on 15 none does.
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
			want = h2s - 1
		}
		if h2s < 6 || gaps != want || body[0].Kind == "gap" {
			t.Fatalf("%d rows: %d of %d H2s have a gap above\n%s", lines, gaps, h2s, viewText(body))
		}
		press(a, "esc")
	}
}

// docs/ux.md §3, §4, §13: Help's THIS PAGE names every key its page acts
// on, not only the footer's. A key acts when it changes the frame, from
// where the page starts or after the opposite key, or what Esc shows next
// (so a Name prompt's Home and End must leave the list's cursor alone).
// o, where it opens the notice row's offer, is said in the same words
// everywhere.
func TestHelpListsEveryKeyThePageTakes(t *testing.T) {
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
		{"plans", func(a *app) { latched(a); press(a, "esc"); offered(a) }},
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
			// acts says whether key changes the frame, or what Esc shows next;
			// two apps' frames differ by their repos' names.
			frame := func(a *app) string { return strings.ReplaceAll(a.frame(), filepath.Base(a.plans.Repo), "<repo>") }
			acts := func(before []string, key string) bool {
				a := start()
				press(a, before...)
				was := frame(a)
				if press(a, key); frame(a) != was {
					return true
				}
				b := start()
				press(b, before...)
				press(a, "esc")
				press(b, "esc")
				return frame(a) != frame(b)
			}
			if first.offerOpens() && (!listed["o"] || !strings.Contains(strings.Join(strings.Fields(text), " "), offerHelp.what)) {
				t.Errorf("%d×%d %s: o opens the offer, but THIS PAGE doesn't say %q\n%s", size[0], size[1], pg.page, offerHelp.what, text)
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

// docs/ux.md §4: the footer is " · "-joined key verb segments. While they
// don't fit, the lowest-ranked one goes (? help never), and the order
// never changes; each counts as its widest form, so what the cursor
// selects never changes which ones show.
func TestFitFooter(t *testing.T) {
	enter := seg("enter about", "enter details")
	segs := []FooterSeg{{Text: "↑↓ select"}, enter, {Text: "esc plans"}, {Text: "tab agent"}, helpSeg}
	for w, want := range map[int]string{
		78: "↑↓ select · enter about · esc plans · tab agent · ? help",
		58: "↑↓ select · enter about · esc plans · tab agent · ? help",
		57: "enter about · esc plans · tab agent · ? help",
		45: "enter about · esc plans · ? help",
		33: "enter about · ? help",
		21: "? help",
		5:  "? he…",
	} {
		if got := strings.Join(fitFooter(segs, w), footerSep); got != want {
			t.Errorf("%d cells: %q want %q", w, got, want)
		}
	}
	if got := fitFooter(nil, 38); got != nil {
		t.Errorf("%q", got)
	}
	// One muted line, a cell short of the edge; the quit warning is muted
	// too, and no word is bold.
	for _, line := range []LogLine{footerLine(segs, 38), footerLine([]FooterSeg{{Text: "ctrl+c again to quit"}}, 38)} {
		if line.Kind != "muted" || len(line.Spans) != 0 {
			t.Errorf("%+v", line)
		}
	}
	if got := footerLine(segs, 58).Text; got != "enter about · esc plans · tab agent · ? help" {
		t.Errorf("58 cells keep 57: %q", got)
	}
}

// docs/ux.md §4: each screen's footer at 24, 38, 60 and 79 cells, keys in
// words joined by " · ", ? help last wherever ? opens Help, and esc
// naming where it goes. An offer never changes a footer: o is named by
// its notice.
func TestFootersPinned(t *testing.T) {
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
	type want [4]string // at 24, 38, 60 and 79 cells
	sizes := [4]int{24, 80, 60, 121}
	home := want{"enter open · ? help", "enter open · tab agent · ? help", "↑↓ select · enter open · tab agent · ? help", "↑↓ select · enter open · type to find · tab agent · ? help"}
	create := want{"enter create · ? help", "enter create · tab agent · ? help", "↑↓ select · enter create · tab agent · ? help", "↑↓ select · enter create · type to find · tab agent · ? help"}
	outline := want{"enter details · ? help", "enter details · esc plans · ? help", "↑↓ select · enter details · esc plans · tab agent · ? help", "↑↓ select · enter details · esc plans · tab agent · ? help"}
	detail := want{"esc outline · ? help", "esc outline · tab agent · ? help", "↑↓ scroll · esc outline · tab agent · ? help", "↑↓ scroll · esc outline · tab agent · ? help"}
	agent := want{"tab plan · ? help", "↑↓ scroll · tab plan · ? help", "↑↓ scroll · home oldest · end live · tab plan · ? help", "↑↓ scroll · home oldest · end live · tab plan · ? help"}
	for _, tc := range []struct {
		name  string
		setup func(a *app)
		want  want
	}{
		{"plan list", func(a *app) { press(a, "esc") }, home},
		{"plan list on a folder", func(a *app) { press(a, "esc"); pick(a, PlanRowDir) }, home},
		{"plan list on + New plan", func(a *app) { press(a, "esc"); pick(a, PlanRowNew) }, create},
		{"plan list on + New folder", func(a *app) { press(a, "esc"); pick(a, PlanRowMkdir) }, create},
		{"plan list, offer", func(a *app) { latched(a); offered(a); press(a, "esc") }, home},
		{"find", func(a *app) { press(a, "esc"); typeText(a, "send") }, want{"enter open", "enter open · esc clear · tab agent", "↑↓ select · enter open · esc clear · tab agent", "↑↓ select · enter open · esc clear · tab agent"}},
		{"find on + Create", func(a *app) { press(a, "esc"); typeText(a, "zz-new") }, want{"enter create", "enter create · esc clear · tab agent", "↑↓ select · enter create · esc clear · tab agent", "↑↓ select · enter create · esc clear · tab agent"}},
		{"browse, a top folder", func(a *app) { press(a, "esc"); pick(a, PlanRowDir); press(a, "\r") }, want{"enter open · ? help", "enter open · esc plans · ? help", "↑↓ select · enter open · esc plans · tab agent · ? help", "↑↓ select · enter open · type to filter · esc plans · tab agent · ? help"}},
		{"browse, deeper", func(a *app) {
			press(a, "esc")
			pick(a, PlanRowDir)
			press(a, "\r")
			pick(a, PlanRowDir)
			press(a, "\r")
		}, want{"enter open · ? help", "enter open · esc up · ? help", "↑↓ select · enter open · esc up · tab agent · ? help", "↑↓ select · enter open · type to filter · esc up · tab agent · ? help"}},
		{"browse, on + New plan", func(a *app) { press(a, "esc"); pick(a, PlanRowDir); press(a, "\r"); pick(a, PlanRowNew) }, want{"enter create · ? help", "enter create · esc plans · ? help", "↑↓ select · enter create · esc plans · tab agent · ? help", "↑↓ select · enter create · type to filter · esc plans · tab agent · ? help"}},
		{"plan list, /help typed", func(a *app) { press(a, "esc"); typeText(a, "/help") }, want{"enter help · esc clear", "enter help · esc clear · tab agent", "enter help · esc clear · tab agent", "enter help · esc clear · tab agent"}},
		{"name prompt", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r") }, want{"enter create · ? help", "enter create · esc cancel · ? help", "enter create · esc cancel · tab agent · ? help", "enter create · esc cancel · tab agent · ? help"}},
		{"name prompt, named", func(a *app) { press(a, "esc"); pick(a, PlanRowNew); press(a, "\r"); typeText(a, "x") }, want{"enter create", "enter create · esc clear · tab agent", "enter create · esc clear · tab agent", "enter create · esc clear · tab agent"}},
		{"outline", func(a *app) {}, outline},
		{"outline, title row", func(a *app) { press(a, "home") }, want{"enter about · ? help", "enter about · esc plans · ? help", "↑↓ select · enter about · esc plans · tab agent · ? help", "↑↓ select · enter about · esc plans · tab agent · ? help"}},
		{"outline, offer", func(a *app) { latched(a); press(a, "esc"); typeText(a, "sendoff"); press(a, "\r"); offered(a) }, outline},
		{"task detail", func(a *app) { press(a, "\r") }, detail},
		{"about", func(a *app) { press(a, "home", "\r") }, detail},
		{"detail, offer", func(a *app) { latched(a); press(a, "esc"); typeText(a, "sendoff"); press(a, "\r", "\r"); offered(a) }, detail},
		{"agent", func(a *app) { press(a, "\t"); a.ingest([]watch.Event{watch.NewEvent("claude", "text", "done", nil)}) }, agent},
		{"agent, scrolled", scrolled, agent},
		{"agent, offer", func(a *app) { latched(a); scrolled(a); offered(a) }, agent},
		{"help on PLAN", func(a *app) { press(a, "?") }, want{"esc close · tab agent", "↑↓ scroll · esc close · tab agent", "↑↓ scroll · esc close · tab agent", "↑↓ scroll · esc close · tab agent"}},
		{"help on AGENT", func(a *app) { press(a, "\t", "?") }, want{"esc close · tab plan", "↑↓ scroll · esc close · tab plan", "↑↓ scroll · esc close · tab plan", "↑↓ scroll · esc close · tab plan"}},
		{"quit armed", func(a *app) { press(a, CtrlC) }, want{"ctrl+c again to quit", "ctrl+c again to quit", "ctrl+c again to quit", "ctrl+c again to quit"}},
	} {
		for i, cols := range sizes {
			a, _, _ := cujApp(t)
			writePlan(t, a.plans.Repo, "cuj/sub/deep.rex.json", demoPlan) // a folder to go deeper into
			a.plans.walk = planWalk{}
			a.settle(cols, 24)
			tc.setup(a)
			a.settle(cols, 24)
			rows := a.frameRows().Rows
			if got := textOf(rows[len(rows)-1], cols); got != tc.want[i] {
				t.Errorf("%s at %d cells:\n got %q\nwant %q", tc.name, PaneTextWidth(cols), got, tc.want[i])
			}
		}
	}
	// The pages that start at their top: ↑↓ scroll only while they run
	// past the screen.
	for _, tc := range []struct {
		name       string
		empty      bool
		setup      func(a *app)
		cols, rows int
		want       string
	}{
		{"wait", true, func(a *app) {}, 80, 24, "esc plans · tab agent · ? help"},
		{"agent, empty", true, func(a *app) { press(a, "\t") }, 80, 24, "tab plan · ? help"},
		{"agent, empty, runs past", true, func(a *app) { a.health, a.hidden = notWired(), 4; press(a, "\t") }, 80, 12, "↑↓ scroll · tab plan · ? help"},
		{"plan list, no plan open", true, func(a *app) { press(a, "esc") }, 80, 24, "enter create · esc wait · ? help"},
		{"plan list, no plan open, wide", true, func(a *app) { press(a, "esc") }, 121, 24, "↑↓ select · enter create · type to find · esc wait · tab agent · ? help"},
	} {
		a := testApp(t)
		tc.setup(a)
		a.settle(tc.cols, tc.rows)
		rows := a.frameRows().Rows
		if got := textOf(rows[len(rows)-1], tc.cols); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// footerKeys is a footer as its keys alone: what the cursor may change in
// it (a verb) taken out.
func footerKeys(footer string) string {
	var keys []string
	for _, s := range strings.Split(footer, footerSep) {
		key, _, _ := strings.Cut(s, " ")
		keys = append(keys, key)
	}
	return strings.Join(keys, footerSep)
}

// docs/ux.md §4: moving the cursor never changes which segments the footer
// shows, on any list screen, at any row.
func TestFooterNeverMovesWithTheCursor(t *testing.T) {
	screens := []struct {
		name  string
		setup func(a *app)
	}{
		{"plan list", func(a *app) { seedKinds(t, a); press(a, "esc") }},
		{"find", func(a *app) { seedKinds(t, a); press(a, "esc"); typeText(a, "o") }},
		{"browse", func(a *app) {
			seedKinds(t, a)
			writePlan(t, a.plans.Repo, "ux/sub/deep.rex.json", demoPlan)
			writePlan(t, a.plans.Repo, "ux/notes.md", "notes")
			a.plans.walk = planWalk{}
			press(a, "esc")
			a.plans.Screen, a.plans.Dir, a.plans.fresh = ScreenBrowse, filepath.Join(a.plans.Root, "ux"), true
			a.plans.Rebuild()
		}},
		{"outline", func(a *app) {
			os.WriteFile(a.plans.Current, []byte(redesignPlan), 0o644)
			a.reloadDoc()
		}},
	}
	for _, size := range [][2]int{{24, 12}, {80, 24}, {100, 24}, {121, 32}} {
		for _, s := range screens {
			a, _, _ := cujApp(t)
			s.setup(a)
			a.settle(size[0], size[1])
			press(a, "home")
			footer := func() string { rows := a.frameRows().Rows; return textOf(rows[len(rows)-1], size[0]) }
			first, seen := footer(), map[string]bool{}
			for i := 0; i < 60; i++ {
				if got := footer(); footerKeys(got) != footerKeys(first) {
					t.Errorf("%s %d×%d: the cursor changed the footer:\n%q\n%q", s.name, size[0], size[1], first, got)
					break
				}
				seen[footer()] = true
				press(a, "down")
			}
			if len(seen) < 2 && s.name != "find" {
				t.Errorf("%s %d×%d: no row changed what enter does: %v", s.name, size[0], size[1], seen)
			}
		}
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

// docs/ux.md §4, §9a: hard drift is a notice on PLAN until you look at
// AGENT, which lists it; drift seen on AGENT as it happens is no notice.
func TestHardDriftNoticeGoesOnceAgentIsSeen(t *testing.T) {
	a, c, _ := openDemo(t, driftDemo, 120, 32)
	a.ingest([]watch.Event{edit(a, c, time.Second, "README.md")})
	if got := a.notice().Text; got != "off pin · README.md · outside card-highlight files" {
		t.Fatalf("hard drift on PLAN: %q", got)
	}
	press(a, "\t", "\t")
	if got := a.notice().Text; got != "" {
		t.Fatalf("a look at AGENT sees it: %q", got)
	}
	press(a, "\t")
	a.ingest([]watch.Event{edit(a, c, 2*time.Second, "docs/other.md")})
	press(a, "\t")
	if got := a.notice().Text; got != "" || !strings.Contains(viewText(a.logView(78)), "hard docs/other.md") {
		t.Fatalf("drift seen on AGENT is in its block, and no notice: %q", got)
	}
}

// docs/ux.md §4: the outline's title row keeps its 2-cell gutter whether
// the cursor is on it or not, so moving the cursor changes only the
// gutter: never which crumbs, meta or label row 0 shows.
func TestTitleRowNeverMovesWithTheCursor(t *testing.T) {
	for _, title := range []string{"Sixteen cell ttl", "Send-off demo", "A very long plan title that cannot fit"} {
		page := Page{Crumbs: []string{title}, Meta: metaSpans("1/3 done · following"), Short: metaSpans("1/3"), Selectable: true}
		for width := 24; width <= 80; width++ {
			off := plainRow(titleLine(page, width, PanePlans), width)
			page.Selected = true
			on := plainRow(titleLine(page, width, PanePlans), width)
			page.Selected = false
			if cutCells(off, 2) != "  " || cutCells(on, 2) != "❯ " || tailCells(off, cellLen(off)-2) != tailCells(on, cellLen(on)-2) {
				t.Errorf("%q at %d cells:\noff %q\non  %q", title, width, off, on)
			}
		}
	}
	a, _, _ := openDemo(t, strings.Replace(demoPlan, `"title": "Send-off demo"`, `"title": "Sixteen cell ttl"`, 1), 80, 24)
	row0 := func() string { return textOf(a.frameRows().Rows[0], 80) }
	on := ""
	for _, key := range []string{"home", "down", "down", "up", "up"} {
		press(a, key)
		got := row0()
		if on == "" {
			on = got
		}
		if tailCells(got, cellLen(got)-2) != tailCells(on, cellLen(on)-2) {
			t.Fatalf("%s moved row 0: %q → %q", key, on, got)
		}
	}
}
