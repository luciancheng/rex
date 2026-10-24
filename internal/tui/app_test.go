package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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

// With no plan yet, start waits for the agent to write one (docs/ux.md
// §11); Create is in the plan list, not the front door.
func TestAppStartsOnTheWaitScreen(t *testing.T) {
	a := testApp(t)
	if a.pane != PanePlans || a.at != atWait || a.plans.Current != "" {
		t.Fatalf("pane=%d at=%d current=%q", a.pane, a.at, a.plans.Current)
	}
	frame := a.frame()
	if !strings.Contains(frame, "no plan yet · tell your agent to make one") || strings.Contains(frame, "Create a plan") {
		t.Fatalf("%s", frame)
	}
	if !strings.Contains(frame, "esc plans  tab agent  ? help") || !strings.Contains(frame, "PLANS › waiting for a plan") {
		t.Fatalf("the footer says where to go\n%s", frame)
	}
	press(a, "esc")
	if a.at != atLibrary || !strings.Contains(a.frame(), "▌ + New plan") {
		t.Fatalf("Esc is the plan list, + New plan in it\n%s", a.frame())
	}
}

// The plan list's draft waits across Tab; the tape has no composer, so
// typing there builds nothing (docs/ux.md §2).
func TestAppTabKeepsThePlanListsDraft(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	typeText(a, "abc")
	press(a, "\t")
	if a.pane != PaneChat || strings.Contains(a.frame(), "abc") {
		t.Fatalf("pane=%d: the tape shows no draft\n%s", a.pane, a.frame())
	}
	typeText(a, "hi")
	press(a, "\t")
	if a.pane != PanePlans || a.draft != "abc" {
		t.Fatalf("plan draft lost across Tab, or the tape typed into it: %q", a.draft)
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
		events = append(events, watch.NewEvent("cursor", "text", fmt.Sprintf("reply %d", i), nil))
	}
	a.ingest(events)
	for i := 0; i < 500; i++ {
		press(a, "up")
	}
	total := len(BuildLogView(a.log, PaneTextWidth(a.columns), a.viewOpt()))
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
	press(a, "esc")
	typeText(a, "/breakdance")
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

// AGENT has no composer and no notes: words typed there go nowhere, and
// its body takes the composer's rows (docs/ux.md §2, §10).
func TestAppAgentHasNoComposer(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	typeText(a, "a note")
	press(a, "\r", PastePrefix+"pasted")
	if len(a.log) != 0 || a.draft != "" || a.turn != TurnIdle {
		t.Fatalf("log %#v draft %q", a.log, a.draft)
	}
	if f := a.frame(); strings.Contains(f, "╭") || strings.Contains(f, "a note") || strings.Contains(f, "pasted") {
		t.Fatalf("no box, nothing typed\n%s", f)
	}
	if PaneBodyHeight(a.lines) != a.lines-4 {
		t.Fatalf("AGENT's body is every row but rows 0 to 2 and the footer: %d", PaneBodyHeight(a.lines))
	}
	if PaneBodyHeight(11) != 11-3 {
		t.Fatalf("under 12 rows it has no page title: %d", PaneBodyHeight(11))
	}
}

// The plan list holds the only composer (docs/ux.md §2): / finds or
// filters, + names; the screens without one draw no box at all.
func TestThePlanListsComposerPrefix(t *testing.T) {
	a := testApp(t)
	if f := a.frame(); strings.Contains(f, "╭") {
		t.Fatalf("the wait screen has no composer\n%s", f)
	}
	press(a, "esc")
	if f := a.frame(); !strings.Contains(f, "│ / ") {
		t.Fatalf("Home finds\n%s", f)
	}
	press(a, "\r")
	if f := a.frame(); a.plans.Screen != ScreenCreate || !strings.Contains(f, "│ + ") {
		t.Fatalf("the Name prompt names\n%s", f)
	}
	press(a, "\t")
	if f := a.frame(); a.pane != PaneChat || strings.Contains(f, "╭") {
		t.Fatalf("the tape has no composer\n%s", f)
	}
}

// Opening or creating a plan lands on its outline (docs/ux.md §6); it
// used to jump to the tape.
func TestAppOpeningAPlanLandsOnItsOutline(t *testing.T) {
	a := testApp(t)
	press(a, "esc", "\r")
	if a.plans.Screen != ScreenCreate {
		t.Fatalf("screen=%d", a.plans.Screen)
	}
	typeText(a, "alpha")
	press(a, "\r")
	if a.pane != PanePlans || a.at != atOutline || !strings.HasSuffix(a.plans.Current, "alpha.rex.json") {
		t.Fatalf("pane=%d at=%d current=%q", a.pane, a.at, a.plans.Current)
	}
	if !strings.Contains(a.frame(), "No tasks yet") {
		t.Fatal("a new plan's outline says it has no tasks")
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
		view := BuildLogView(a.log, PaneTextWidth(a.columns), a.viewOpt())
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

// Keys from one read are applied one at a time: a filter letter and enter in
// the same read open the row the filter selected.
func TestAppKeysFromOneReadSettleInOrder(t *testing.T) {
	a := testApp(t)
	press(a, "esc")
	root := filepath.Join(a.plans.Repo, "rex", "plans", "team")
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

// docs/ux.md §4: the tab bar's cue says the other place has news: from
// PLAN, • for a reply and ! for an ask or a turn's error; from AGENT, •
// for a plan change and ! for a plan Rex can't read. Showing a place
// clears its cue, and a cue never moves you.
func TestCue(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	sel := a.outline.sel
	unmoved := func(pane PaneKind, at planAt) {
		t.Helper()
		if a.pane != pane || a.at != at || a.outline.sel != sel {
			t.Fatalf("a cue moved you: pane=%d at=%d sel=%q", a.pane, a.at, a.outline.sel)
		}
	}
	head := func() string { return a.frameRows().Rows[0] }
	for i, ev := range []watch.Event{
		{Source: "claude", Kind: "thought", Text: "thinking it over"},
		{Source: "claude", Kind: "command", Text: "go test ./...", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "error", Text: "exit status 1", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "result", Text: "ok", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "command", Text: "Read README.md", Meta: map[string]any{"tool": "Read", "path": "README.md"}},
		{Source: "claude", Kind: "text", Text: "a reply in another repo", Meta: map[string]any{"cwd": t.TempDir()}},
		{Source: "rex", Kind: "text", Text: "a feed line posing as Rex"},
	} {
		if a.ingest([]watch.Event{ev}); a.unseen[PaneChat] != "" || !strings.Contains(head(), "[PLAN] AGENT") {
			t.Fatalf("event %d (%s) lit the cue: %q", i, ev.Kind, head())
		}
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "Done: one block now."}})
	if !strings.Contains(head(), "[PLAN]•AGENT") {
		t.Fatalf("a reply waits on AGENT: %q", head())
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
	a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "Another reply."}})
	if !strings.Contains(head(), "[PLAN]!AGENT") {
		t.Fatalf("an ask outranks a reply: %q", head())
	}
	unmoved(PanePlans, atOutline)
	press(a, "\t")
	if a.unseen[PaneChat] != "" || !strings.Contains(head(), "PLAN [AGENT]") {
		t.Fatalf("showing AGENT clears its cue: %q", head())
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "Seen as it comes."}})
	if a.unseen[PaneChat] != "" {
		t.Fatal("nothing builds up for the place on screen")
	}
	// Scrolled up on AGENT, the agent edits the plan: PLAN lights, and
	// neither place, selection nor scroll moves.
	var many []watch.Event
	for i := 0; i < 40; i++ {
		many = append(many, watch.NewEvent("claude", "text", "line "+string(rune('a'+i%26))+string(rune('a'+i/26)), nil))
	}
	a.ingest(many)
	press(a, "up", "up")
	body := func() string { rows := a.frameRows().Rows; return strings.Join(rows[3:len(rows)-1], "\n") }
	before := body()
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)), 0o644)
	later(a, c)
	if !strings.Contains(head(), "PLAN•[AGENT]") || a.unseen[PanePlans] != "•" {
		t.Fatalf("a plan change lights PLAN: %q", head())
	}
	if body() != before || !strings.Contains(a.frameRows().Rows[2], " new") {
		t.Fatalf("the scrolled view moved, or doesn't count what came in below\n%s", a.frame())
	}
	unmoved(PaneChat, atOutline)
	os.WriteFile(path, []byte(demoPlan[:100]), 0o644)
	later(a, c)
	later(a, c)
	if !strings.Contains(head(), "PLAN![AGENT]") {
		t.Fatalf("a plan Rex can't read: %q", head())
	}
	// Fixed, nothing needs you any more, but the plan did change.
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"Outline view"`, `"Outline view, fixed"`, 1)), 0o644)
	later(a, c)
	if !strings.Contains(head(), "PLAN•[AGENT]") || !strings.HasPrefix(a.statusLine().Text, "plan updated") {
		t.Fatalf("a fixed plan is news, not an alarm: %q, row 1 %q", head(), a.statusLine().Text)
	}
	press(a, "\t")
	if a.unseen[PanePlans] != "" || !strings.Contains(head(), "[PLAN] AGENT") {
		t.Fatalf("showing PLAN clears its cue: %q", head())
	}
	// A turn's own error needs you too; a failed tool call doesn't.
	a.ingest([]watch.Event{{Source: "claude", Kind: "error", Text: "the turn failed"}})
	if a.unseen[PaneChat] != "!" {
		t.Fatalf("a turn's error: %q", a.unseen[PaneChat])
	}
	// From AGENT, a plan that opens with no hook (the cold-start scan)
	// lights PLAN too.
	w := testApp(t)
	wc := &clock{t: time.Unix(1_800_000_000, 0)}
	w.now = wc.now
	press(w, "\t")
	writePlan(t, w.plans.Repo, "found.rex.json", demoPlan)
	later(w, wc)
	if w.at != atOutline || w.pane != PaneChat || w.unseen[PanePlans] != "•" {
		t.Fatalf("an attach from the scan: at=%d pane=%d cue=%q", w.at, w.pane, w.unseen[PanePlans])
	}
	// A plan the agent writes that Rex can't read opens with its error at
	// once, and no poll will say so: it lights !.
	broken := writePlan(t, w.plans.Repo, "cuj/broken.rex.json", "{nope")
	w.ingest([]watch.Event{wrote(w.plans.Repo, broken)})
	if w.plans.Current != broken || w.unseen[PanePlans] != "!" || !strings.Contains(w.frameRows().Rows[0], "PLAN![AGENT]") {
		t.Fatalf("an unreadable attach: current=%q cue=%q row 1 %q", w.plans.Current, w.unseen[PanePlans], w.statusLine().Text)
	}
	// The agent then writes a plan Rex can read: it replaces the broken one,
	// so nothing needs you any more.
	good := writePlan(t, w.plans.Repo, "cuj/good.rex.json", demoPlan)
	w.ingest([]watch.Event{wrote(w.plans.Repo, good)})
	if w.plans.Current != good || w.unseen[PanePlans] != "•" || !strings.Contains(w.frameRows().Rows[0], "PLAN•[AGENT]") {
		t.Fatalf("a readable attach puts out the !: current=%q cue=%q", w.plans.Current, w.unseen[PanePlans])
	}
	// Latched, seen from AGENT, the agent writes another plan: row 1
	// offers it and PLAN lights, and nothing moves.
	o, _, opened := openDemo(t, demoPlan, 120, 32)
	press(o, "down", "\t")
	osel := o.outline.sel
	next := writePlan(t, o.plans.Repo, "cuj/next.rex.json", demoPlan)
	o.ingest([]watch.Event{wrote(o.plans.Repo, next)})
	if o.offer == "" || o.unseen[PanePlans] != "•" || !strings.Contains(o.frameRows().Rows[0], "PLAN•[AGENT]") {
		t.Fatalf("an offer lights PLAN: offer=%q cue=%q %q", o.offer, o.unseen[PanePlans], o.frameRows().Rows[0])
	}
	if o.pane != PaneChat || o.at != atOutline || o.outline.sel != osel || o.plans.Current != opened || o.follow {
		t.Fatalf("an offer moved you: pane=%d at=%d sel=%q current=%q", o.pane, o.at, o.outline.sel, o.plans.Current)
	}
	// The cue lives in memory only.
	if data, _ := os.ReadFile(a.statePath); strings.Contains(string(data), "unseen") || strings.Contains(string(data), "•") {
		t.Fatalf("state.json holds a cue: %s", data)
	}
}
