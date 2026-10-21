package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func browseApp(t *testing.T) *app {
	t.Helper()
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	if err := os.MkdirAll(filepath.Join(root, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seed.rex.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
	for i, row := range a.plans.Rows {
		if row.Kind == PlanRowOpen {
			a.plans.Cursor = i
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
	if !strings.HasSuffix(a.plans.Dir, "team") {
		t.Fatalf("→ enters a folder: %q", a.plans.Dir)
	}
	typeText(a, "zz")
	press(a, "esc")
	if a.draft != "" || !strings.HasSuffix(a.plans.Dir, "team") {
		t.Fatal("first Esc clears the filter and stays")
	}
	press(a, "esc")
	if a.plans.Dir != a.plans.Root || !a.plans.Browsing() {
		t.Fatal("then Esc climbs one folder")
	}
	press(a, "esc")
	if a.plans.Screen != ScreenHome {
		t.Fatal("then to Home")
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
	press(a, "\r")
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
	press(a, "\t")
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
	press(a, "\t")
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

func TestNotesAreLocalAndLabelled(t *testing.T) {
	a := testApp(t)
	feed := filepath.Join(t.TempDir(), "session.jsonl")
	a.feed = feed
	press(a, "\t")
	typeText(a, "remember the cards")
	press(a, "\r")
	view := viewText(a.logView(78))
	if !strings.Contains(view, "── note · local") || strings.Contains(view, "── reply") {
		t.Fatalf("a local note must not look like an agent reply\n%s", view)
	}
	if _, err := os.Stat(feed); !os.IsNotExist(err) {
		t.Fatal("notes are never written to the feed")
	}
	if a.turn != TurnIdle {
		t.Fatal("notes do not start a turn")
	}
}

func TestIdleIsNoLongerAVerb(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	a.ingest([]watch.Event{watch.NewEvent("cursor", "user", "go", nil)})
	typeText(a, "idle")
	press(a, "\r")
	if a.turn != TurnThinking {
		t.Fatal("typing idle must not end the agent's turn")
	}
}

func TestHelpOverlay(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	typeText(a, "?")
	if !strings.Contains(a.frame(), "⏎ help") {
		t.Fatal("footer previews the verb")
	}
	press(a, "\r")
	frame := a.frame()
	if !strings.Contains(frame, "── help") || !strings.Contains(frame, "rex never") || !strings.Contains(frame, "esc close") {
		t.Fatalf("help overlay\n%s", frame)
	}
	press(a, "\t")
	if strings.Contains(a.frame(), "── help") {
		t.Fatal("overlay belongs to the tape")
	}
	press(a, "\t")
	if !strings.Contains(a.frame(), "── help") {
		t.Fatal("Tab keeps the overlay open in its place")
	}
	press(a, "esc")
	if strings.Contains(a.frame(), "── help") {
		t.Fatal("Esc closes it")
	}
}

func TestLibraryTextIsLiteralUnlessSlashed(t *testing.T) {
	a := testApp(t)
	typeText(a, "help")
	press(a, "\r")
	if a.overlay[a.pane] != overlayNone {
		t.Fatal("plain 'help' on Home is not a verb")
	}
	a.draft = ""
	typeText(a, "/help")
	press(a, "\r")
	if a.overlay[a.pane] != overlayHelp {
		t.Fatal("/help forces the verb")
	}
	press(a, "esc")
	typeText(a, "?")
	press(a, "\r")
	if a.overlay[a.pane] != overlayHelp {
		t.Fatal("a lone ? is help everywhere")
	}
}

func TestPlaceholdersAndPromptCap(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	if !strings.Contains(a.frame(), "note (not sent) · ?⏎ help") {
		t.Fatal("tape placeholder")
	}
	long := strings.Repeat("word ", 400)
	a.ingest([]watch.Event{watch.NewEvent("cursor", "user", long, nil)})
	view := viewText(a.logView(78))
	if !strings.Contains(view, "(+") || strings.Count(view, "word") > 6*20 {
		t.Fatalf("a long prompt is capped\n%s", view)
	}
}

func TestSubmittingJumpsToLive(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	var events []watch.Event
	for i := 0; i < 80; i++ {
		events = append(events, watch.NewEvent("cursor", "command", strings.Repeat("x", i+1), map[string]any{"tool": "shell"}))
	}
	a.ingest(events)
	press(a, "home")
	if a.scroll == 0 {
		t.Fatal("setup: scrolled")
	}
	typeText(a, "note")
	press(a, "\r")
	if a.scroll != 0 {
		t.Fatal("a submitted note jumps to live")
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
	typeText(a, "/zap")
	press(a, "\r")
	if a.plans.Current != "" || a.plans.Screen != ScreenHome || !strings.Contains(a.plans.Err, "'/zap' is not a verb") {
		t.Fatalf("screen %d current %q err %q", a.plans.Screen, a.plans.Current, a.plans.Err)
	}
}

func TestHelpOwnsTheComposer(t *testing.T) {
	a := testApp(t)
	a.settle(80, 24)
	press(a, "\t")
	typeText(a, "?")
	press(a, "\r")
	if a.placeholder() != "esc to close" {
		t.Fatal(a.placeholder())
	}
	typeText(a, "breakdance")
	press(a, "\r")
	if a.sequence.Name == "breakdance" || a.overlay[PaneChat] != overlayHelp || a.draft != "" {
		t.Fatal("a word typed in Help is dropped, never run underneath")
	}
	for i := 0; i < 50; i++ {
		press(a, "down")
	}
	top := a.overlayScroll[PaneChat]
	press(a, "up")
	if a.overlayScroll[PaneChat] != top-1 {
		t.Fatalf("scroll is clamped, so ↑ works at once: %d -> %d", top, a.overlayScroll[PaneChat])
	}
	for _, line := range a.helpBody(PaneTextWidth(80)) {
		if strings.Contains(line.Text, "…") || cellLen(line.Text) > PaneTextWidth(80) {
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
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "alpha.rex.json"), []byte("{}"), 0o644)
	a.plans.Screen, a.plans.Dir = ScreenBrowse, root
	a.plans.Rebuild()
	typeText(a, "?")
	press(a, "\r")
	typeText(a, "zz")
	if a.plans.Filter != "" {
		t.Fatalf("filter %q changed under Help", a.plans.Filter)
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
	press(a, "\t")
	typeText(a, strings.Repeat("x", draftCap-4))
	if !strings.Contains(a.frame(), "252/256") {
		t.Fatal("near the cap the count shows")
	}
	b := testApp(t)
	press(b, "\t")
	if !strings.Contains(b.frame(), "(note (not sent) · ?⏎ help)") {
		t.Fatal("without color the placeholder is in parentheses")
	}
}

func TestCtrlCInHelpDropsWhatWasTyped(t *testing.T) {
	a := testApp(t)
	root := filepath.Join(a.plans.Repo, "rex", "plans")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "alpha.rex.json"), []byte("{}"), 0o644)
	a.plans.Screen, a.plans.Dir = ScreenBrowse, root
	a.plans.Rebuild()
	typeText(a, "?")
	press(a, "\r")
	typeText(a, "zz")
	press(a, CtrlC)
	if a.overlay[PanePlans] != overlayNone || a.draft != "" || a.plans.Filter != "" {
		t.Fatalf("overlay %d draft %q filter %q", a.overlay[PanePlans], a.draft, a.plans.Filter)
	}
	a.settle(80, 24)
	typeText(a, "?")
	press(a, "\r")
	if f := a.frameRows().Rows; !strings.Contains(f[len(f)-1], "tab tape") {
		t.Fatalf("help keeps its must-have tab at 38 cells: %q", f[len(f)-1])
	}
	press(a, "esc")
	typeText(a, "/zap")
	if !strings.Contains(a.frame(), "⏎ not a verb") {
		t.Fatal("the footer says a /word isn't a verb before ⏎")
	}
}
