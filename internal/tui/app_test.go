package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/luciancheng/rex/internal/mascot"
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

// statusLine is row 1 as a terminal under blankFrom rows draws it: the
// notice while one shows, else the page's context (docs/ux.md §4).
func (a *app) statusLine() StatusLine {
	if n := a.notice(); n.Text != "" {
		return n
	}
	return a.context()
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
	if !strings.Contains(frame, "esc plans · tab agent · ? help") || !strings.Contains(frame, "Plans › Waiting for a plan") {
		t.Fatalf("the footer says where to go\n%s", frame)
	}
	press(a, "esc")
	if a.at != atLibrary || !strings.Contains(a.frame(), "❯ + New plan") {
		t.Fatalf("Esc is the plan list, + New plan in it\n%s", a.frame())
	}
}

// docs/ux.md §4: your agent asking outranks a toast and a new plan on
// the notice row; once the turn goes on past the question they show, and
// o takes the offer again.
func TestAToastAndAnOfferShowOnceTheAskClears(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	next := writePlan(t, a.plans.Repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(a.plans.Repo, next)})
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Allow Bash(go test ./...)?"}})
	done := strings.Replace(demoPlan, `"status": "active",
      "depends_on": ["plans-ui"]`, `"status": "done",
      "depends_on": ["plans-ui"]`, 1)
	os.WriteFile(path, []byte(done), 0o644)
	later(a, c)
	if _, k := a.noticeOf(); k != noticeNeeds {
		t.Fatalf("the ask outranks the done toast and the offer: %v %q", k, a.notice().Text)
	}
	if press(a, "o"); a.plans.Current != path {
		t.Fatal("o does nothing while the ask hides the offer")
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "result", Text: "ok", Meta: map[string]any{"tool": "Bash"}}})
	if got := a.notice().Text; got != "✔ card-highlight done · next up load-outline" {
		t.Fatalf("the done toast shows once the ask clears: %q", got)
	}
	c.t = c.t.Add(toastFor)
	a.tick()
	if got := a.notice().Text; got != "Your agent wrote Send-off demo (cuj/next) · o opens it" {
		t.Fatalf("then the offer: %q", got)
	}
	if press(a, "o"); a.plans.Current != next || !a.follow {
		t.Fatalf("o takes it: %q", a.plans.Current)
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
	total := len(BuildLogView(a.log, PaneTextWidth(a.columns, a.lines), a.viewOpt()))
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

// docs/ux.md §12: the mascot glances once when a plan opens (start's, an
// attach, a pick in the plan list, the offer taken with o), on the frame
// clock: it holds glanceTicks ticks under the pose's own frames, a key
// neither eats it nor moves it, and a reload of the open plan never plays
// it. The turn keeps the typing pose, which never looks aside, so every
// glance on screen is an open's.
func TestAPlanOpenGlancesOnce(t *testing.T) {
	a, c, path := cujApp(t) // start opened the CUJ plan
	repo := a.plans.Repo
	a.ingest([]watch.Event{watch.NewEvent("claude", "user", "go", nil)})
	look := strings.Join(stagedMascot(mascot.Glance, a.color, mascot.StageSize, false), "\n")
	glancing := func() bool {
		var left []string
		for _, row := range a.frameRows().Rows[:len(stagedMascot(mascot.Glance, a.color, mascot.StageSize, false))] {
			left = append(left, cutCells(row, mascot.StageSize))
		}
		return strings.Join(left, "\n") == look
	}
	// once ticks the clock until the glance has had its time, and long
	// after: it shows from the open through its glanceTicks-th frame,
	// then never again.
	once := func(what string) {
		t.Helper()
		if !glancing() {
			t.Fatalf("%s: the frame the plan opens on glances", what)
		}
		for i := 1; i <= 4*glanceTicks; i++ {
			if a.tick(); glancing() != (i < glanceTicks) {
				t.Fatalf("%s: tick %d glances %v", what, i, glancing())
			}
		}
	}
	once("start")
	if a.sequence.Name != "typing" {
		t.Fatalf("the turn's pose: %s", a.sequence.Name)
	}

	// The agent saves the open plan: a reload, polled or written.
	for i, src := range []string{strings.Replace(demoPlan, "Outline view", "Outline view, again", 1), demoPlan} {
		os.WriteFile(path, []byte(src), 0o644)
		if i == 0 {
			later(a, c)
		} else {
			a.ingest([]watch.Event{wrote(repo, path)})
		}
		for n := 0; n < 2*glanceTicks; n++ {
			if glancing() || a.glance != 0 {
				t.Fatalf("reload %d: no glance (tick %d)", i, n)
			}
			a.tick()
		}
	}

	// The agent writes another plan, which Rex follows.
	next := writePlan(t, repo, "cuj/next.rex.json", demoPlan)
	a.ingest([]watch.Event{wrote(repo, next)})
	if a.plans.Current != next {
		t.Fatalf("attach: %q", a.plans.Current)
	}
	// A key is the key's: it moves the selection and leaves the glance
	// to the clock.
	sel, left := a.outline.sel, a.glance
	press(a, "down")
	if a.outline.sel == sel || a.glance != left {
		t.Fatalf("a key during the glance: sel %q → %q, glance %d → %d", sel, a.outline.sel, left, a.glance)
	}
	once("attach")

	// A pick in the plan list, which latches; then o takes the offer.
	press(a, "esc")
	typeText(a, "sendoff-demo")
	press(a, "\r")
	if a.plans.Current != path || a.follow {
		t.Fatalf("pick: %q follow=%v", a.plans.Current, a.follow)
	}
	once("pick")
	a.ingest([]watch.Event{wrote(repo, next)})
	if a.glance != 0 || a.offer == "" {
		t.Fatalf("latched, the write is only offered: glance %d offer %q", a.glance, a.offer)
	}
	press(a, "o")
	if a.plans.Current != next {
		t.Fatalf("o: %q", a.plans.Current)
	}
	once("offer")

	// Where the mascot doesn't show, an open plays nothing.
	a.settle(109, 24)
	press(a, "esc")
	typeText(a, "sendoff-demo")
	press(a, "\r")
	if a.plans.Current != path || a.glance != 0 {
		t.Fatalf("109 columns: glance %d", a.glance)
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
	if PaneBodyHeight(a.lines) != a.lines-5 {
		t.Fatalf("AGENT's body is every row but rows 0 to 2, the notice row and the footer: %d", PaneBodyHeight(a.lines))
	}
	if PaneBodyHeight(11) != 11-3 {
		t.Fatalf("under 12 rows it has no blank row 2 or notice row: %d", PaneBodyHeight(11))
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
	if row := a.frameRows().Rows[0]; !strings.Contains(row, "alpha ") || !strings.Contains(row, "no tasks") {
		t.Fatalf("row 0 names the plan: %q", row)
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
		view := BuildLogView(a.log, PaneTextWidth(a.columns, a.lines), a.viewOpt())
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

// docs/ux.md §4: from AGENT only your agent's question or its turn's
// error reaches PLAN, as a notice that holds until you look at AGENT; a
// reply, a step, a failed tool call, another repo's event or Rex's own
// line reach nothing. Nothing moves you, and row 0's label never changes.
// A toast raised while AGENT is on screen waits for PLAN; an error, a
// held plan and an offer show on AGENT too.
func TestAgentNeedsYouNotice(t *testing.T) {
	a, c, path := openDemo(t, demoPlan, 120, 32)
	press(a, "down")
	sel := a.outline.sel
	unmoved := func(pane PaneKind, at planAt) {
		t.Helper()
		if a.pane != pane || a.at != at || a.outline.sel != sel {
			t.Fatalf("a notice moved you: pane=%d at=%d sel=%q", a.pane, a.at, a.outline.sel)
		}
	}
	rows := func() []string { return a.frameRows().Rows }
	label := func() string {
		return strings.TrimSpace(textOf(rows()[0], a.columns)[cellLen(textOf(rows()[0], a.columns))-12:])
	}
	noticeRow := func() string { r := rows(); return textOf(r[len(r)-2], a.columns) }
	for i, ev := range []watch.Event{
		{Source: "claude", Kind: "thought", Text: "thinking it over"},
		{Source: "claude", Kind: "command", Text: "go test ./...", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "error", Text: "exit status 1", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "result", Text: "ok", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "command", Text: "Read README.md", Meta: map[string]any{"tool": "Read", "path": "README.md"}},
		{Source: "claude", Kind: "text", Text: "a reply in another repo", Meta: map[string]any{"cwd": t.TempDir()}},
		{Source: "claude", Kind: "question", Text: "another repo asks", Meta: map[string]any{"cwd": t.TempDir()}},
		{Source: "rex", Kind: "question", Text: "a feed line posing as Rex"},
		{Source: "claude", Kind: "text", Text: "Done: one block now."},
	} {
		if a.ingest([]watch.Event{ev}); a.needs != "" || noticeRow() != "" || label() != "plan · agent" {
			t.Fatalf("event %d (%s) reached PLAN: %q %q", i, ev.Kind, noticeRow(), label())
		}
	}
	const asks = "Your agent is asking you something · tab agent to read it"
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
	if got := noticeRow(); got != asks || label() != "plan · agent" {
		t.Fatalf("an ask holds on PLAN: %q %q", got, label())
	}
	c.t = c.t.Add(time.Minute)
	if a.tick(); noticeRow() != asks {
		t.Fatal("an ask holds while the turn waits on you, not for a while")
	}
	// Answered in the agent itself, the turn goes on past the question:
	// the ask is done, as row 1's turn word says, whatever comes next.
	// A file look AGENT doesn't draw is no answer.
	for _, next := range []watch.Event{
		{Source: "claude", Kind: "command", Text: "go test ./...", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "result", Text: "ok", Meta: map[string]any{"tool": "Bash"}},
		{Source: "claude", Kind: "text", Text: "Another reply."},
		{Source: "claude", Kind: "idle", Text: "turn completed"},
		{Source: "claude", Kind: "user", Text: "go on"},
	} {
		a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file, " + next.Kind + "?"}})
		a.ingest([]watch.Event{{Source: "claude", Kind: "command", Text: "Read README.md", Meta: map[string]any{"tool": "Read", "path": "README.md"}}})
		if noticeRow() != asks {
			t.Fatalf("a hidden file look cleared the ask before %s", next.Kind)
		}
		if a.ingest([]watch.Event{next}); a.needs != "" || noticeRow() != "" {
			t.Fatalf("a %s after the ask leaves %q", next.Kind, noticeRow())
		}
	}
	// In one batch, too: the ask and its answer, in order.
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Allow Bash?"}, {Source: "claude", Kind: "result", Text: "ok", Meta: map[string]any{"tool": "Bash"}}})
	if a.needs != "" {
		t.Fatal("an ask answered in the same batch")
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Which file?"}})
	unmoved(PanePlans, atOutline)
	press(a, "\t")
	if a.needs != "" || label() != "plan · agent" {
		t.Fatalf("looking at AGENT sees the ask: %q %q", a.needs, label())
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "question", Text: "Seen as it comes."}})
	if a.needs != "" {
		t.Fatal("nothing builds up for the place on screen")
	}
	// Scrolled up on AGENT, the agent edits the plan: the toast waits for
	// PLAN, and neither place, selection nor scroll moves.
	var many []watch.Event
	for i := 0; i < 40; i++ {
		many = append(many, watch.NewEvent("claude", "text", "line "+string(rune('a'+i%26))+string(rune('a'+i/26)), nil))
	}
	a.ingest(many)
	press(a, "up", "up")
	body := func() string { r := rows(); return strings.Join(r[3:len(r)-2], "\n") }
	before := body()
	os.WriteFile(path, []byte(strings.Replace(demoPlan, `"status": "pending"`, `"status": "skipped"`, 1)), 0o644)
	later(a, c)
	if noticeRow() != "" || !a.toastLive() || !strings.HasPrefix(a.toast.Text, "plan updated") {
		t.Fatalf("a plan change waits for PLAN: %q %q", noticeRow(), a.toast.Text)
	}
	if body() != before || !strings.Contains(textOf(rows()[0], a.columns), "↓ ") {
		t.Fatalf("the scrolled view moved, or doesn't say what is below\n%s", a.frame())
	}
	unmoved(PaneChat, atOutline)
	c.t = c.t.Add(time.Hour)
	if a.tick(); !a.toastLive() {
		t.Fatal("a toast waits for PLAN however long AGENT is on screen")
	}
	press(a, "\t")
	if got := noticeRow(); !strings.HasPrefix(got, "plan updated") {
		t.Fatalf("back on PLAN the toast shows: %q", got)
	}
	c.t = c.t.Add(toastFor - time.Millisecond)
	if a.tick(); !strings.HasPrefix(noticeRow(), "plan updated") {
		t.Fatal("its time starts on PLAN")
	}
	c.t = c.t.Add(time.Millisecond)
	if a.tick(); noticeRow() != "" {
		t.Fatalf("then it goes: %q", noticeRow())
	}
	// A plan Rex can't read is an error on both places.
	press(a, "\t")
	os.WriteFile(path, []byte(demoPlan[:100]), 0o644)
	later(a, c)
	later(a, c)
	if got := noticeRow(); !strings.HasPrefix(got, "✗ plan JSON error") {
		t.Fatalf("a plan Rex can't read, seen from AGENT: %q", got)
	}
	os.WriteFile(path, []byte(demoPlan), 0o644)
	later(a, c)
	press(a, "\t")
	// A turn's own error needs you too; a failed tool call doesn't. It
	// holds over what comes after it in the feed, until the next prompt.
	a.ingest([]watch.Event{{Source: "claude", Kind: "error", Text: "the turn failed"}})
	if got := noticeRow(); a.needs != "error" || got != "✗ Your agent's turn ended with an error · tab agent" {
		t.Fatalf("a turn's error: %q", got)
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "text", Text: "a late reply"}, {Source: "claude", Kind: "idle", Text: "turn completed"}})
	if a.needs != "error" {
		t.Fatal("a turn's error holds until the next prompt")
	}
	a.ingest([]watch.Event{{Source: "claude", Kind: "user", Text: "try again"}})
	if a.needs != "" || strings.Contains(noticeRow(), "turn ended") {
		t.Fatalf("the next prompt clears a turn's error: %q", noticeRow())
	}
	// Latched, seen from AGENT, the agent writes another plan: the notice
	// offers it there, and nothing moves.
	o, _, opened := openDemo(t, demoPlan, 120, 32)
	press(o, "down", "\t")
	osel := o.outline.sel
	next := writePlan(t, o.plans.Repo, "cuj/next.rex.json", demoPlan)
	o.ingest([]watch.Event{wrote(o.plans.Repo, next)})
	if r := o.frameRows().Rows; o.offer == "" || textOf(r[len(r)-2], o.columns) != "Your agent wrote Send-off demo (cuj/next) · o opens it" {
		t.Fatalf("an offer shows on AGENT: offer=%q %q", o.offer, textOf(r[len(r)-2], o.columns))
	}
	if o.pane != PaneChat || o.at != atOutline || o.outline.sel != osel || o.plans.Current != opened || o.follow {
		t.Fatalf("an offer moved you: pane=%d at=%d sel=%q current=%q", o.pane, o.at, o.outline.sel, o.plans.Current)
	}
	// The notice lives in memory only.
	if data, _ := os.ReadFile(a.statePath); strings.Contains(string(data), "needs") || strings.Contains(string(data), "asking") {
		t.Fatalf("state.json holds a notice: %s", data)
	}
}
