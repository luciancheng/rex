package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func testApp(t *testing.T) *app {
	t.Helper()
	return newApp("yellow", false, "idle", NewPlanBrowser(t.TempDir()), 120, 32)
}

func press(a *app, keys ...string) {
	for _, key := range keys {
		a.handleKey(key)
		a.settle(a.columns, a.lines)
	}
}

func typeText(a *app, text string) {
	for _, r := range text {
		press(a, string(r))
	}
}

// The tests below pin today's behaviour before the UX slices change it on
// purpose (docs/ux.md); a later slice that flips one says so.

func TestAppStartsOnPlansHome(t *testing.T) {
	a := testApp(t)
	if a.pane != PanePlans || a.plans.Screen != ScreenHome {
		t.Fatalf("pane=%d screen=%d", a.pane, a.plans.Screen)
	}
	if frame := a.frame(); !strings.Contains(frame, "Create a plan") || !strings.Contains(frame, "plans") {
		t.Fatalf("%s", frame)
	}
}

func TestAppTabKeepsTheChatDraft(t *testing.T) {
	a := testApp(t)
	typeText(a, "abc")
	press(a, "\t")
	if a.pane != PaneChat || a.draft != "" {
		t.Fatalf("pane=%d draft=%q", a.pane, a.draft)
	}
	typeText(a, "hi")
	press(a, "\t")
	if a.pane != PanePlans || a.draft != "" {
		t.Fatalf("plans draft should start empty, got %q", a.draft)
	}
	press(a, "\t")
	if a.draft != "hi" {
		t.Fatalf("chat draft lost: %q", a.draft)
	}
}

func TestAppEscOnHomeGoesToTape(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	if a.pane != PaneChat {
		t.Fatal("today Esc on Home switches to the tape (docs/ux.md changes this)")
	}
}

func TestAppCtrlCWindow(t *testing.T) {
	a := testApp(t)
	press(a, "\t", CtrlC)
	if !a.quitPending || a.quit {
		t.Fatal("first ctrl+c arms")
	}
	if !strings.Contains(a.frame(), "press ctrl+c again to exit") {
		t.Fatal("footer should warn")
	}
	press(a, "x")
	if a.quitPending {
		t.Fatal("typing disarms")
	}
	press(a, CtrlC)
	for i := 0; i < QuitWindowTicks; i++ {
		a.tick()
	}
	if a.quitPending {
		t.Fatal("window should time out")
	}
	press(a, CtrlC, CtrlC)
	if !a.quit {
		t.Fatal("second ctrl+c quits")
	}
}

func TestAppScrollClampsToTheLog(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	var events []watch.Event
	for i := 0; i < 60; i++ {
		events = append(events, watch.NewEvent("cursor", "command", fmt.Sprintf("ls %d", i), map[string]any{"tool": "shell"}))
	}
	a.ingest(events)
	for i := 0; i < 500; i++ {
		press(a, "up")
	}
	total := len(BuildLogView(a.log, PaneTextWidth(a.columns)))
	if want := ClampScroll(total, PaneBodyHeight(a.lines), total); a.scroll != want || want == 0 {
		t.Fatalf("scroll=%d want=%d", a.scroll, want)
	}
	press(a, "end")
	if a.scroll != 0 {
		t.Fatal("end returns to live")
	}
}

func TestAppIngestDrivesTheTurn(t *testing.T) {
	a := testApp(t)
	a.ingest([]watch.Event{watch.NewEvent("cursor", "user", "go", nil)})
	if a.turn != TurnThinking || a.sequence.Name != "typing" {
		t.Fatalf("turn=%d anim=%s", a.turn, a.sequence.Name)
	}
	a.ingest([]watch.Event{watch.NewEvent("cursor", "text", "done", nil)})
	if a.turn != TurnIdle || a.sequence.Name != "idle" {
		t.Fatalf("turn=%d anim=%s", a.turn, a.sequence.Name)
	}
}

func TestAppBreakdancePlaysOnceThenResumes(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	typeText(a, "breakdance")
	press(a, "\r")
	if a.sequence.Name != "breakdance" {
		t.Fatalf("anim=%s", a.sequence.Name)
	}
	for i := 0; i < 10000 && a.sequence.Name == "breakdance"; i++ {
		a.tick()
	}
	if a.sequence.Name != "idle" {
		t.Fatalf("should resume idle, got %s", a.sequence.Name)
	}
}

func TestAppComposerNoteIsLoggedLocally(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	typeText(a, "a note")
	press(a, "\r")
	if len(a.log) != 1 || a.log[0].Source != "user" || a.log[0].Text != "a note" {
		t.Fatalf("%#v", a.log)
	}
	if a.turn != TurnIdle {
		t.Fatal("a note does not start a turn")
	}
}

func TestAppOpeningAPlanSwitchesToChat(t *testing.T) {
	a := testApp(t)
	press(a, "\r")
	if a.plans.Screen != ScreenCreate {
		t.Fatalf("screen=%d", a.plans.Screen)
	}
	typeText(a, "alpha")
	press(a, "\r")
	if a.pane != PaneChat || !strings.HasSuffix(a.plans.Current, "alpha.rex.json") {
		t.Fatalf("pane=%d current=%q", a.pane, a.plans.Current)
	}
	if !strings.Contains(a.frame(), "rex/plans/alpha.rex.json") {
		t.Fatal("header shows the plan")
	}
}

func TestAppResizeIsPickedUp(t *testing.T) {
	a := testApp(t)
	if !a.settle(80, 24) || a.columns != 80 || a.lines != 24 {
		t.Fatal("resize")
	}
	if a.settle(80, 24) {
		t.Fatal("no change, no redraw")
	}
}
