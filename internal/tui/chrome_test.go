package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if !strings.HasPrefix(rows[0], "REX tape") {
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
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); segs[1] != "⏎ create" {
		t.Fatalf("on Create: %q", segs)
	}
	open := false
	for i, row := range b.Rows {
		if row.Kind == PlanRowOpen {
			b.Cursor, open = i, true
		}
	}
	if !open {
		t.Fatal("with a plan, Home has an Open card")
	}
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); segs[1] == "⏎ create" {
		t.Fatalf("on Open a plan: %q", segs)
	}
}

// docs/ux.md §12: under 7 rows the plan list's hidden composer takes no
// ⏎, so its footer offers none; the outline has no composer and ⏎ still
// acts there, so its footer keeps it.
func TestShortTerminalFooterOffersNoEnter(t *testing.T) {
	repo := t.TempDir()
	b := NewPlanBrowser(repo)
	for i, row := range b.Rows {
		if row.Kind == PlanRowNew {
			b.Cursor = i
		}
	}
	if segs, _ := footerSegments(ComposeOptions{Pane: PanePlans, Plans: b}, 0); !strings.Contains(strings.Join(segs, " "), "⏎") {
		t.Fatalf("on Create the footer offers ⏎: %q", segs)
	}
	rows := frameAt(t, 100, 6, ComposeOptions{Pane: PanePlans, Plans: b, Composer: true})
	if strings.Contains(rows[len(rows)-1], "⏎") {
		t.Fatalf("composer hidden, no ⏎: %q", rows[len(rows)-1])
	}

	a, _, _ := openDemo(t, demoPlan, 120, 32)
	a.settle(120, 6)
	rows = a.frameRows().Rows
	if footer := rows[len(rows)-1]; !strings.Contains(footer, "⏎ detail") {
		t.Fatalf("the outline keeps ⏎ detail at 6 rows: %q", footer)
	}
	if press(a, "\r"); a.at != atTaskDetail {
		t.Fatalf("⏎ still opens the task: at=%d", a.at)
	}
}

// docs/ux.md §12: under 7 rows typing is ignored in the plan list, so its
// footer drops "type to find"; ⏎ on an empty draft still opens the
// selected card, so "⏎ open" stays until a hidden draft would make ⏎ a
// blind submit.
func TestShortPlanListFooterSaysWhatWorks(t *testing.T) {
	a, _, _ := cujApp(t)
	press(a, "esc")
	a.settle(50, 6)
	footer := func() string { rows := a.frameRows().Rows; return rows[len(rows)-1] }
	if f := footer(); strings.Contains(f, "type to") || !strings.Contains(f, "⏎ open") || !strings.Contains(f, "? help") {
		t.Fatalf("Home at 50×6: %q", f)
	}
	if typeText(a, "zz"); a.draft != "" || strings.Contains(footer(), "type to") {
		t.Fatalf("typing is ignored, and the footer doesn't offer it: %q %q", a.draft, footer())
	}
	if press(a, "\r"); a.at != atOutline {
		t.Fatalf("⏎ opens Continue, as the footer says: at=%d", a.at)
	}

	press(a, "esc")
	a.settle(120, 32)
	typeText(a, "send")
	a.settle(50, 6)
	if f := footer(); strings.Contains(f, "⏎") || strings.Contains(f, "? help") || !strings.Contains(f, "esc clear") {
		t.Fatalf("a hidden find: ⏎ and ? would act on it blind: %q", f)
	}
	a.settle(120, 32)
	press(a, "esc", "home", "down", "down", "\r") // the find cleared, then Open a plan
	if a.plans.Screen != ScreenBrowse {
		t.Fatalf("browse: %d", a.plans.Screen)
	}
	a.settle(50, 6)
	if f := footer(); strings.Contains(f, "type to") || !strings.Contains(f, "⏎ open") {
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

func TestHeaderOnEveryScreen(t *testing.T) {
	b := NewPlanBrowser(t.TempDir())
	b.Current = b.Root + "/cuj/sendoff-demo.rex.json"
	for _, pane := range []PaneKind{PanePlans, PaneChat} {
		rows := frameAt(t, 120, 32, ComposeOptions{Pane: pane, Plans: b, Status: "thinking", PlanRel: b.CurrentRel()})
		want := "REX " + placeWord(pane)
		if !strings.Contains(rows[0], want) || !strings.Contains(rows[0], "thinking · rex/plans/cuj/sendoff-demo.rex.json") {
			t.Fatalf("pane %d header %q", pane, rows[0])
		}
	}
	narrow := frameAt(t, 80, 24, ComposeOptions{Pane: PaneChat, Status: "running", PlanRel: "rex/plans/cuj/sendoff-demo.rex.json"})
	if !strings.Contains(narrow[0], "running · cuj/sendoff-demo") {
		t.Fatalf("38-col header should drop rex/plans/ and .rex.json: %q", narrow[0])
	}
}

func TestShortPlanPath(t *testing.T) {
	path := "rex/plans/cuj/sendoff-demo.rex.json"
	for room, want := range map[int]string{
		40: path,
		30: "cuj/sendoff-demo.rex.json",
		20: "cuj/sendoff-demo",
		10: "…doff-demo",
	} {
		if got := shortPlanPath(path, room); got != want || cellLen(got) > room {
			t.Fatalf("room %d: %q want %q", room, got, want)
		}
	}
}

func TestFootersKeepMustHavesAt38Cells(t *testing.T) {
	b := NewPlanBrowser(t.TempDir())
	type screen struct {
		name string
		opt  ComposeOptions
		must []string
	}
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, "rex", "plans"), 0o755)
	os.WriteFile(filepath.Join(repo, "rex", "plans", "a.rex.json"), []byte("{}"), 0o644)
	withPlans := NewPlanBrowser(repo)
	for i, row := range withPlans.Rows {
		if row.Kind == PlanRowLatest {
			withPlans.Cursor = i
		}
	}
	finding := NewPlanBrowser(repo)
	finding.Find = "zz-new"
	finding.Rebuild()
	findOpen := NewPlanBrowser(repo)
	findOpen.Find = "a"
	findOpen.Rebuild()
	browse := *b
	browse.Screen = ScreenBrowse
	create := *b
	create.Screen = ScreenCreate
	screens := []screen{
		{"home (no plans)", ComposeOptions{Pane: PanePlans, Plans: b, Composer: true}, []string{"plans", "⏎ create", "tab tape"}},
		{"home", ComposeOptions{Pane: PanePlans, Plans: withPlans, Composer: true}, []string{"plans", "⏎ open", "tab tape"}},
		{"find (create)", ComposeOptions{Pane: PanePlans, Plans: finding, Composer: true, Draft: "zz-new"}, []string{"find", "⏎ create", "esc clear", "tab tape"}},
		{"find (open)", ComposeOptions{Pane: PanePlans, Plans: findOpen, Composer: true, Draft: "a"}, []string{"find", "⏎ open", "esc clear", "tab tape"}},
		{"browse", ComposeOptions{Pane: PanePlans, Plans: &browse, Composer: true}, []string{"open", "⏎ open", "esc back", "tab tape"}},
		{"create", ComposeOptions{Pane: PanePlans, Plans: &create, Composer: true}, []string{"new plan", "⏎ create", "esc cancel"}},
		{"tape", ComposeOptions{Pane: PaneChat}, []string{"tape", "tab plan", "↑↓ scroll"}},
		{"quit", ComposeOptions{Pane: PaneChat, QuitPending: true}, []string{"ctrl+c again to quit"}},
	}
	for _, s := range screens {
		rows := frameAt(t, 80, 24, s.opt)
		footer := rows[len(rows)-1]
		for _, m := range s.must {
			if !strings.Contains(footer, m) {
				t.Fatalf("%s footer at 38 cells lost %q: %q", s.name, m, footer)
			}
		}
		if strings.HasSuffix(strings.TrimRight(footer, " "), "·") {
			t.Fatalf("%s footer cut mid-segment: %q", s.name, footer)
		}
	}
	wait := testApp(t) // no plan yet: the wait screen (docs/ux.md §11)
	wait.settle(80, 24)
	if rows := wait.frameRows().Rows; !strings.Contains(rows[len(rows)-1], "no plan · esc plans · tab tape") {
		t.Fatalf("wait footer at 38 cells: %q", rows[len(rows)-1])
	}
	// The open plan's screens, which have no composer: no verbs, the
	// must-haves at 38 cells.
	a, _, _ := openDemo(t, demoPlan, 80, 24)
	for _, step := range []struct {
		keys []string
		must string
	}{
		{nil, "outline · tab tape · esc plans"},
		{[]string{"\r"}, "detail · esc outline · ↑↓ scroll"},
		{[]string{"esc", "\t"}, "tape · tab plan · ↑↓ scroll"},
		{[]string{"?"}, "help · esc close · tab plan"},
	} {
		press(a, step.keys...)
		rows := a.frameRows().Rows
		if footer := rows[len(rows)-1]; !strings.Contains(footer, step.must) || strings.Contains(footer, "p⏎") {
			t.Fatalf("footer at 38 cells %q, want %q", footer, step.must)
		}
	}
	for _, w := range []int{24, 38, 78} {
		got := fitFooter([]string{"outline", "tab tape", "esc plans", "⏎ detail", "? help"}, w)
		if cellLen(got) > w || strings.HasSuffix(got, "·") {
			t.Fatalf("width %d: %q", w, got)
		}
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

func TestHeaderColorsAskAndError(t *testing.T) {
	opt := ComposeOptions{Enabled: true, Pane: PaneChat, Status: "ask", Log: []watch.Event{}}
	opt.Columns, opt.Lines = 120, 32
	row := composeRows("idle", "yellow", opt).Rows[0]
	if !strings.Contains(row, "\033[38;2;232;78;78mask") {
		t.Fatalf("ask should use the error color: %q", row)
	}
}
