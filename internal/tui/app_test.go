package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

// testApp runs in a throwaway git repo so a plan created by a test can
// never land in whatever repo holds the temp dir.
func testApp(t *testing.T) *app {
	t.Helper()
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return newApp("yellow", false, "idle", NewPlanBrowser(repo), 120, 32)
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

func TestAppTabKeepsEachPlacesDraft(t *testing.T) {
	a := testApp(t)
	typeText(a, "abc")
	press(a, "\t")
	if a.pane != PaneChat || a.draft != "" {
		t.Fatalf("pane=%d draft=%q", a.pane, a.draft)
	}
	typeText(a, "hi")
	press(a, "\t")
	if a.pane != PanePlans || a.draft != "abc" {
		t.Fatalf("plan draft lost across Tab: %q", a.draft)
	}
	press(a, "\t")
	if a.draft != "hi" {
		t.Fatalf("tape draft lost: %q", a.draft)
	}
}

func TestAppEscNeverLeavesThePlace(t *testing.T) {
	a := testApp(t)
	press(a, "esc", "esc")
	if a.pane != PanePlans {
		t.Fatal("Esc on Home must not switch to the tape (docs/ux.md §3)")
	}
	press(a, "\t", "esc")
	if a.pane != PaneChat {
		t.Fatal("Esc on the tape must not switch to PLAN")
	}
}

func TestAppCtrlCWindow(t *testing.T) {
	a := testApp(t)
	press(a, "\t", CtrlC)
	if !a.quitPending || a.quit {
		t.Fatal("first ctrl+c arms")
	}
	if !strings.Contains(a.frame(), "ctrl+c again to quit") {
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
	if len(a.log) != 1 || a.log[0].Source != "rex" || a.log[0].Kind != "note" || a.log[0].Text != "a note" {
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

// A scrolled-up tape keeps showing the same lines while new events arrive
// below it.
func TestAppScrolledViewStaysPut(t *testing.T) {
	a := testApp(t)
	var evs []watch.Event
	for i := 0; i < 60; i++ {
		evs = append(evs, watch.NewEvent("cursor", "text", fmt.Sprintf("reply number %d", i), nil))
	}
	a.ingest(evs)
	press(a, "\t", "up", "up", "up", "up", "up")
	if a.scroll != 5 {
		t.Fatalf("scroll %d", a.scroll)
	}
	top := func() string {
		view := BuildLogView(a.log, PaneTextWidth(a.columns))
		end := len(view) - a.scroll
		return view[max(0, end-PaneBodyHeight(a.lines))].Text
	}
	before, lines := top(), a.logLines()
	a.ingest([]watch.Event{watch.NewEvent("cursor", "text", "a new reply", nil)})
	if a.scroll != 5+a.logLines()-lines {
		t.Fatalf("scroll %d after %d new lines", a.scroll, a.logLines()-lines)
	}
	if top() != before {
		t.Fatalf("the view moved: %q -> %q", before, top())
	}
}

// Keys from one read are applied one at a time: a filter letter and ⏎ in
// the same read open the row the filter selected.
func TestAppKeysFromOneReadSettleInOrder(t *testing.T) {
	a := testApp(t)
	root := filepath.Join(a.plans.Repo, "rex", "plans")
	os.MkdirAll(root, 0o755)
	for _, name := range []string{"alpha.rex.json", "beta.rex.json"} {
		os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644)
	}
	a.plans.Screen = ScreenBrowse
	a.plans.Dir = root
	a.plans.Rebuild()
	for i, row := range a.plans.Rows {
		if row.Name == "alpha.rex.json" {
			a.plans.Cursor = i // the unfiltered row under the cursor
		}
	}
	a.keys([]string{"b", "e", "\r"})
	if !strings.HasSuffix(a.plans.Current, "beta.rex.json") {
		t.Fatalf("opened %q", a.plans.Current)
	}
}

func TestCtrlLDisarmsQuitAndResizeRepaints(t *testing.T) {
	a := testApp(t)
	press(a, CtrlC, "\x0c")
	if a.quitPending || !a.repaint {
		t.Fatal("Ctrl+L repaints and disarms quit like any other key")
	}
	a.repaint = false
	a.settle(100, 30)
	if !a.repaint {
		t.Fatal("a resize asks for a full repaint")
	}
}

// The TUI paints event kinds. Vendors live in internal/watch.
func TestTUINeverBranchesOnVendor(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	vendor := regexp.MustCompile(`(?i)"(cursor|claude|codex|opencode)"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := vendor.Find(data); m != nil {
			t.Errorf("%s names a vendor: %s", f, m)
		}
	}
}
