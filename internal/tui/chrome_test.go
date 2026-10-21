package tui

import (
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
	opt.Columns, opt.Lines, opt.AnimationName = cols, lines, "idle"
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
	press(a, "\t", "x", "\r")
	if a.pane != PanePlans || a.draft != "" {
		t.Fatal("keys must not act on a UI that isn't visible")
	}
	press(a, CtrlC, CtrlC)
	if !a.quit {
		t.Fatal("ctrl+c still quits")
	}
}

func TestShortTerminalComposer(t *testing.T) {
	rows := frameAt(t, 100, 12, ComposeOptions{Pane: PaneChat, Draft: "hi"})
	if strings.Contains(strings.Join(rows, ""), "╭") {
		t.Fatal("under 16 rows the composer is one bare row")
	}
	if !strings.Contains(rows[10], "> hi") {
		t.Fatalf("composer row %q", rows[10])
	}
	a := testApp(t)
	a.settle(100, 6)
	press(a, "\t", "x")
	if a.draft != "" {
		t.Fatal("under 7 rows printable keys are ignored")
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
	browse := *b
	browse.Screen = ScreenBrowse
	create := *b
	create.Screen = ScreenCreate
	screens := []screen{
		{"home", ComposeOptions{Pane: PanePlans, Plans: b}, []string{"plans", "⏎ open", "tab tape"}},
		{"browse", ComposeOptions{Pane: PanePlans, Plans: &browse}, []string{"open", "⏎ open", "esc back", "tab tape"}},
		{"create", ComposeOptions{Pane: PanePlans, Plans: &create}, []string{"new plan", "⏎ create", "esc cancel"}},
		{"tape", ComposeOptions{Pane: PaneChat}, []string{"tape", "tab plan", "↑↓ scroll"}},
		{"typing", ComposeOptions{Pane: PaneChat, Draft: "x"}, []string{"tape", "⏎ note (local, not sent)"}},
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
	for _, w := range []int{24, 38, 78} {
		got := fitFooter([]string{"outline", "tab tape", "esc plans", "⏎ detail", "?⏎ help"}, w)
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
	opt.Columns, opt.Lines, opt.AnimationName = 120, 32, "idle"
	row := composeRows("idle", "yellow", opt).Rows[0]
	if !strings.Contains(row, "\033[38;2;232;78;78mask") {
		t.Fatalf("ask should use the error color: %q", row)
	}
}
