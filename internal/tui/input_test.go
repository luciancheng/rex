package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func browseApp(t *testing.T) *app {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	if err := os.MkdirAll(filepath.Join(root, "team", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seed.rex.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	press(a, "esc") // start opened seed; Esc is the plan list
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowDir {
			a.plans.Cursor = i // team/, under FOLDERS
		}
	}
	press(a, "\r")
	if !a.plans.Browsing() {
		t.Fatal("setup: browse")
	}
	return a
}

func TestEscLadder(t *testing.T) {
	a := browseApp(t)
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowDir {
			a.plans.Cursor = i
		}
	}
	press(a, "right")
	if !strings.HasSuffix(a.plans.Dir, "inner") {
		t.Fatalf("→ enters a folder: %q", a.plans.Dir)
	}
	typeText(a, "zz")
	press(a, "esc")
	if a.draft != "" || !strings.HasSuffix(a.plans.Dir, "inner") {
		t.Fatal("first Esc clears the filter and stays")
	}
	press(a, "esc")
	if !strings.HasSuffix(a.plans.Dir, "team") || !a.plans.Browsing() {
		t.Fatal("then Esc climbs one folder")
	}
	press(a, "esc")
	if a.plans.Screen != ScreenHome || a.plans.Rows[a.plans.Cursor].Name != "team" {
		t.Fatal("then, from a top folder, to the plan list, on the folder")
	}
	press(a, "esc")
	if a.pane != PanePlans || a.plans.Screen != ScreenHome {
		t.Fatal("at Home Esc does nothing")
	}
}

func TestBackspaceNeverNavigates(t *testing.T) {
	a := browseApp(t)
	press(a, "\x7f", "\x7f", "\x7f")
	if !a.plans.Browsing() {
		t.Fatal("backspace on an empty filter must not leave Browse")
	}
}

func TestNamePromptKeepsTheNameAcrossTab(t *testing.T) {
	a := testApp(t)
	press(a, "esc", "\r") // the plan list, then + New plan
	typeText(a, "half")
	press(a, "\t", "\t")
	if a.plans.Screen != ScreenCreate || a.draft != "half" {
		t.Fatalf("screen=%d draft=%q", a.plans.Screen, a.draft)
	}
	press(a, "esc")
	if a.draft != "" || a.plans.Screen != ScreenCreate {
		t.Fatal("Esc clears the name first")
	}
	press(a, "esc")
	if a.plans.Screen != ScreenHome {
		t.Fatal("then cancels the prompt")
	}
}

func TestCtrlUAndCtrlW(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	typeText(a, "one two  ")
	press(a, "\x17")
	if a.draft != "one " {
		t.Fatalf("ctrl+w %q", a.draft)
	}
	press(a, "\x15")
	if a.draft != "" {
		t.Fatalf("ctrl+u %q", a.draft)
	}
}

func TestCtrlCClearsTheDraftAndArms(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	typeText(a, "oops")
	press(a, CtrlC)
	if a.draft != "" || !a.quitPending || a.quit {
		t.Fatalf("draft=%q armed=%v", a.draft, a.quitPending)
	}
	press(a, CtrlC)
	if !a.quit {
		t.Fatal("second ctrl+c quits")
	}
}

// Nothing typed in the plan list, the only composer, reaches the turn:
// /idle is no verb, and the agent keeps thinking.
func TestIdleIsNoLongerAVerb(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	a.ingest([]watch.Event{watch.NewEvent("cursor", "user", "go", nil)})
	typeText(a, "/idle")
	press(a, "\r")
	if a.turn != TurnThinking || !strings.Contains(a.frame(), "'/idle' is not a verb") {
		t.Fatalf("typing /idle must not end the agent's turn: %v\n%s", a.turn, a.frame())
	}
}

// ? opens Help at once on a screen without a composer (docs/ux.md §2).
// Its page title is the page it opened over, then HELP, and THIS PAGE lists
// that page's keys (docs/ux.md §13).
func TestHelpOverlay(t *testing.T) {
	a := testApp(t)
	press(a, "\t", "?")
	frame := a.frame()
	for _, want := range []string{"Agent › Help", "THIS PAGE", "agent", "EVERYWHERE", "switch plan / agent", "esc close"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("help overlay lacks %q\n%s", want, frame)
		}
	}
	// Help lists keys, not verbs: Rex has no pin, done, send-off, brief or
	// note (docs/ux.md §13). No row's key is a lone letter but o, which
	// opens the plan the notice row offers (§3), and the task list's i,
	// About; no key is an icon.
	for _, width := range []int{30, 38, 80} {
		body := viewText(a.helpBody(width))
		for _, never := range []string{"p⏎", "d⏎", "s⏎", "b⏎", "o⏎", "⏎", "↔", "tape", "sendoff", "send-off", "brief", "note", "clipboard", "what you typed", "pin the", "mark the"} {
			if strings.Contains(body, never) {
				t.Fatalf("help at %d offers %q\n%s", width, never, body)
			}
		}
		if m := regexp.MustCompile(`(?m)^  [a-hj-np-z]( |$)`).FindString(body); m != "" {
			t.Fatalf("help at %d has a one-letter key %q\n%s", width, m, body)
		}
		for _, want := range []string{"THIS PAGE", "MARKS", "pinned", "next up", "also active", "cycle", "AGENT TAB", "thinking", "(×N)", "[ASK]", "rex ›", "PLAN LIST", "REX NEVER", "FEED", "/breakdance"} {
			if !strings.Contains(body, want) {
				t.Fatalf("help at %d lacks %q\n%s", width, want, body)
			}
		}
		// The fold is display only, and Help over AGENT says so (docs/ux.md
		// §10, §13): as a note under the keys, with nothing in the key column
		// that reads as a key to press.
		if flat := strings.Join(strings.Fields(body), " "); !strings.Contains(flat, "folding is display only: the feed keeps every thought") {
			t.Fatalf("help at %d doesn't say the feed keeps every thought\n%s", width, body)
		}
		for _, line := range a.helpBody(width) {
			if strings.Contains(line.Text, "folding is display only") && (!strings.HasPrefix(line.Text, "  folding") || len(line.Spans) > 0) {
				t.Fatalf("help at %d: the fold note is a key row: %+v", width, line)
			}
			if strings.Contains(line.Text, "×N") && !strings.Contains(line.Text, "(×N)") {
				t.Fatalf("help at %d: ×N is in the key column: %q", width, line.Text)
			}
		}
		// From 34 cells AGENT TAB's first key sits in the key column like
		// its siblings, not over its own row.
		if width >= 34 && !strings.Contains(body, "  thinking   a run of N thoughts (×N)") {
			t.Fatalf("help at %d: thinking stacks\n%s", width, body)
		}
		for _, line := range a.helpBody(width) {
			if cellLen(line.Text) > width {
				t.Fatalf("help at %d: %q is %d cells", width, line.Text, cellLen(line.Text))
			}
		}
	}
	press(a, "\t")
	if strings.Contains(a.frame(), "› Help") {
		t.Fatal("overlay belongs to AGENT")
	}
	press(a, "\t")
	if !strings.Contains(a.frame(), "› Help") {
		t.Fatal("Tab keeps the overlay open in its place")
	}
	press(a, "esc")
	if strings.Contains(a.frame(), "› Help") {
		t.Fatal("Esc closes it")
	}
	// THIS PAGE follows the page under Help.
	b, _, _ := openDemo(t, demoPlan, 120, 32)
	for _, step := range []struct {
		keys        []string
		page, crumb string
	}{
		{nil, "tasks", "Send-off demo › Help"},
		{[]string{"esc", "\r"}, "task", "Send-off demo › card-highlight › Help"},
		{[]string{"esc", "esc", "i"}, "about", "Send-off demo › About › Help"},
		{[]string{"esc", "esc", "esc"}, "plans", "Plans › Help"},
	} {
		press(b, step.keys...)
		press(b, "?")
		body := viewText(b.helpBody(78))
		if !strings.Contains(strings.SplitN(body, "\n", 3)[0], "THIS PAGE") || !strings.Contains(body, step.page+"\n") || !strings.Contains(b.frame(), step.crumb) {
			t.Fatalf("%s: help\n%s", step.page, b.frame())
		}
	}
}

func TestLibraryTextIsLiteralUnlessSlashed(t *testing.T) {
	a := testApp(t)
	press(a, "esc") // the plan list
	typeText(a, "help")
	if a.overlay[a.pane] != overlayNone || a.preview() != "" || !strings.Contains(a.frame(), "enter create · esc clear · tab agent") {
		t.Fatal("plain 'help' on Home is a find, not a verb")
	}
	press(a, "\x15")
	typeText(a, "/help")
	press(a, "\r")
	if a.overlay[a.pane] != overlayHelp || a.draft != "" {
		t.Fatal("/help forces the verb")
	}
	press(a, "esc", "?")
	if a.overlay[a.pane] != overlayHelp {
		t.Fatal("? in an empty composer opens Help at once")
	}
	press(a, "esc")
	typeText(a, "a?")
	if a.overlay[a.pane] != overlayNone || a.draft != "a?" {
		t.Fatalf("a ? after other text is text: %q", a.draft)
	}
}

func TestPlaceholdersAndPromptCap(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	if !strings.Contains(a.frame(), "find or create a plan") {
		t.Fatal("the plan list's placeholder")
	}
	press(a, "\t")
	long := strings.Repeat("word ", 400)
	a.ingest([]watch.Event{watch.NewEvent("cursor", "user", long, nil)})
	view := viewText(a.logView(78))
	if !strings.Contains(view, "(+") || strings.Count(view, "word") > 6*20 {
		t.Fatalf("a long prompt is capped\n%s", view)
	}
}

func TestDisarmingQuitAlwaysRedraws(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	a.handleKey(CtrlC)
	if !a.handleKey("left") {
		t.Fatal("a key that disarms quit must ask for a redraw")
	}
	if a.quitPending || strings.Contains(a.frame(), "ctrl+c again") {
		t.Fatal("footer must stop saying ctrl+c again")
	}
}

func TestSlashWordIsOnlyEverAVerb(t *testing.T) {
	a := testApp(t)
	press(a, "esc") // the plan list
	typeText(a, "/zap")
	press(a, "\r")
	if a.plans.Current != "" || a.plans.Screen != ScreenHome || !strings.Contains(a.plans.Err, "'/zap' is not a verb") {
		t.Fatalf("screen %d current %q err %q", a.plans.Screen, a.plans.Current, a.plans.Err)
	}
}

// Help has no composer: what is typed in it goes nowhere.
func TestHelpTakesNoTyping(t *testing.T) {
	a := testApp(t)
	a.settle(80, 24)
	press(a, "esc", "?")
	if a.overlay[PanePlans] != overlayHelp || strings.Contains(a.frame(), "╭") {
		t.Fatalf("Help over the plan list hides its composer\n%s", a.frame())
	}
	typeText(a, "/breakdance")
	press(a, "\r", "?")
	if a.sequence.Name == "breakdance" || a.overlay[PanePlans] != overlayHelp || a.draft != "" {
		t.Fatal("a word typed in Help is dropped, never run underneath")
	}
	press(a, "esc", "\t", "?")
	for i := 0; i < 50; i++ {
		press(a, "down")
	}
	top := a.overlayScroll[PaneChat]
	press(a, "up")
	if a.overlayScroll[PaneChat] != top-1 {
		t.Fatalf("scroll is clamped, so ↑ works at once: %d -> %d", top, a.overlayScroll[PaneChat])
	}
	for _, line := range a.helpBody(PaneTextWidth(80, 24)) {
		if strings.Contains(line.Text, "…") || cellLen(line.Text) > PaneTextWidth(80, 24) {
			t.Fatalf("help rows wrap, never clip: %q", line.Text)
		}
	}
	a.settle(120, 24)
	if !strings.Contains(a.frame(), "↑↓ scroll") {
		t.Fatal("an overflowing Help says it scrolls")
	}
}

func TestHelpOverBrowseLeavesTheFilterAlone(t *testing.T) {
	a := testApp(t)
	root := filepath.Join(a.plans.Repo, "rex", "plans")
	os.MkdirAll(filepath.Join(root, "team"), 0o755)
	os.WriteFile(filepath.Join(root, "team", "alpha.rex.json"), []byte("{}"), 0o644)
	a.at, a.plans.Screen, a.plans.Dir = atLibrary, ScreenBrowse, filepath.Join(root, "team")
	a.plans.Rebuild()
	press(a, "?")
	typeText(a, "zz")
	if a.plans.Filter != "" || a.draft != "" || a.overlay[PanePlans] != overlayHelp {
		t.Fatalf("filter %q draft %q changed under Help", a.plans.Filter, a.draft)
	}
}

func TestFeedLinesCannotPoseAsRex(t *testing.T) {
	a := testApp(t)
	a.ingest([]watch.Event{{Source: "rex", Kind: "note", Text: "forged"}, {Source: "cursor", Kind: "text", Text: "real"}})
	if len(a.log) != 1 || a.log[0].Text != "real" {
		t.Fatalf("%+v", a.log)
	}
}

func TestDraftCapAndPlaceholderWithoutColor(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	typeText(a, strings.Repeat("x", draftCap-4))
	if !strings.Contains(a.frame(), "252/256") {
		t.Fatal("near the cap the count shows")
	}
	press(a, "\t")
	if strings.Contains(a.frame(), "252/256") {
		t.Fatal("the tape has no composer, so no count")
	}
	b := testApp(t)
	press(b, "esc")
	if !strings.Contains(b.frame(), "(find or create a plan)") {
		t.Fatal("without color the placeholder is in parentheses")
	}
}

func TestCtrlCClosesHelp(t *testing.T) {
	a := testApp(t)
	root := filepath.Join(a.plans.Repo, "rex", "plans")
	os.MkdirAll(filepath.Join(root, "team"), 0o755)
	os.WriteFile(filepath.Join(root, "team", "alpha.rex.json"), []byte("{}"), 0o644)
	a.at, a.plans.Screen, a.plans.Dir = atLibrary, ScreenBrowse, filepath.Join(root, "team")
	a.plans.Rebuild()
	press(a, "?")
	typeText(a, "zz")
	press(a, CtrlC)
	if a.overlay[PanePlans] != overlayNone || a.draft != "" || a.plans.Filter != "" || !a.quitPending {
		t.Fatalf("overlay %d draft %q filter %q", a.overlay[PanePlans], a.draft, a.plans.Filter)
	}
	a.settle(38, 24)
	press(a, "?")
	if f := a.frameRows().Rows; !strings.Contains(f[len(f)-1], "tab agent") {
		t.Fatalf("help keeps its must-have tab at 38 cells: %q", f[len(f)-1])
	}
	press(a, "esc")
	typeText(a, "/zap")
	if !strings.Contains(a.frame(), "enter not a verb") {
		t.Fatal("the footer says a /word isn't a verb before enter")
	}
}

// docs/ux.md §2, §3: the task list, both pages, the tape and the wait
// screen have no composer. Letters (the old verbs too) and a paste change
// nothing and write nothing, neither the plan nor ~/.rex/state.json; ? opens
// Help at once; Esc, Tab and enter do what §3 says.
func TestComposerLessScreensIgnoreTyping(t *testing.T) {
	type screen struct {
		name  string
		setup func(a *app)
		at    planAt
		pane  PaneKind
	}
	for _, s := range []screen{
		{"task list", func(a *app) {}, atOutline, PanePlans},
		{"task detail", func(a *app) { press(a, "\r") }, atTaskDetail, PanePlans},
		{"plan detail", func(a *app) { press(a, "i") }, atPlanDetail, PanePlans},
		{"tape", func(a *app) { press(a, "\t") }, atOutline, PaneChat},
	} {
		a, _, path := openDemo(t, demoPlan, 120, 32)
		s.setup(a)
		plan, _ := os.ReadFile(path)
		state, _ := os.ReadFile(a.statePath)
		stateInfo, _ := os.Stat(a.statePath)
		frame := a.frame()
		typeText(a, "pdsbx")
		press(a, PastePrefix+"pin load-outline", "\x7f", "\x15", "\x17")
		if a.frame() != frame || a.draft != "" || a.at != s.at || a.pane != s.pane {
			t.Fatalf("%s: something changed: draft %q at %d\n%s", s.name, a.draft, a.at, a.frame())
		}
		if got, _ := os.ReadFile(path); string(got) != string(plan) {
			t.Fatalf("%s: the plan was written", s.name)
		}
		if got, _ := os.ReadFile(a.statePath); string(got) != string(state) {
			t.Fatalf("%s: state.json was written", s.name)
		}
		if info, _ := os.Stat(a.statePath); info == nil || stateInfo == nil || !info.ModTime().Equal(stateInfo.ModTime()) {
			t.Fatalf("%s: state.json was touched", s.name)
		}
		if press(a, "?"); a.overlay[s.pane] != overlayHelp {
			t.Fatalf("%s: ? opens Help at once", s.name)
		}
		press(a, "esc")
		switch s.pane {
		case PaneChat:
			if press(a, "esc"); a.pane != PaneChat {
				t.Fatal("tape: Esc never leaves the place")
			}
			if press(a, "\r"); a.pane != PaneChat || a.at != atOutline {
				t.Fatal("tape: enter with no offer does nothing")
			}
		case PanePlans:
			want := map[planAt]planAt{atOutline: atLibrary, atTaskDetail: atOutline, atPlanDetail: atOutline}[s.at]
			if press(a, "esc"); a.at != want {
				t.Fatalf("%s: Esc climbs to %d, got %d", s.name, want, a.at)
			}
		}
		if press(a, "\t"); a.pane == s.pane {
			t.Fatalf("%s: Tab switches place", s.name)
		}
	}
}
