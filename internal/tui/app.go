package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// app is the live screen's state. RunLoop feeds it keys, hook events and
// ticks and paints app.frameRows() through the renderer; tests drive it
// the same way without a TTY.
type app struct {
	color   string
	enabled bool

	sequence   mascot.Animation
	frameIndex int
	hold       int
	spriteName string
	animated   bool

	columns, lines int

	pane  PaneKind
	plans *PlanBrowser
	draft string // the plan list's draft: the only composer (docs/ux.md §2)

	overlay       [2]overlayKind // per place
	overlayScroll [2]int
	// helpOver names the page each place's Help was opened over, for THIS
	// PAGE (docs/ux.md §13): a plan that opens under Help doesn't swap it
	// mid-read. Help's crumbs are the page under it now, the Esc ladder.
	helpOver [2]string
	// unseen is each place's cue on the tab bar while the other is on
	// screen: "•" news, "!" needs you (docs/ux.md §4). Memory only.
	unseen [2]string

	log         []watch.Event
	logRev      int
	view        logViewCache
	scroll      int
	unseenLines int // AGENT lines that arrived below a scrolled view
	turn        Turn
	lastKind    string

	quitPending bool
	quitTicks   int
	quit        bool
	repaint     bool // Ctrl+L asked for a full repaint

	scope  watch.Scope
	hidden int // events from other repos' agents, not shown
	health feedHealth

	doc        *planDoc // the open plan, nil until one is opened
	at         planAt   // which PLAN screen: library, outline or a detail
	outline    outlineView
	toast      StatusLine
	toastUntil time.Time
	now        func() time.Time
	statePath  string
	drift      driftState    // drift from the current pin, shown only
	driftGen   int           // numbers each pin stint's drift block
	driftQueue []watch.Event // live events to judge once their batch is on AGENT
	skill      skillSeen     // whether an agent can learn plans, for the wait screen

	// Attach (docs/ux.md §11): follow opens the plan the agent writes;
	// picking one in the list latches (follow false), and another plan's
	// write is then only offered on row 1.
	follow    bool
	pending   string    // a write to open once the plan list's draft or Name prompt clears
	offer     string    // latched: the plan the agent wrote, enter switches to it
	wrote     string    // the last other plan file written in the batch being ingested
	wroteOpen bool      // the batch wrote the open plan too
	scanned   time.Time // the last cold-start look for a plan file
}

// logViewCache keeps the laid-out AGENT stream for one log revision, width
// and layout (live, tall); the loop needs it several times per tick.
type logViewCache struct {
	rev, width int
	opt        viewOpt
	lines      []LogLine
	ok         bool
}

func newApp(color string, enabled bool, animation string, plans *PlanBrowser, columns, lines int) *app {
	a := &app{color: color, enabled: enabled, pane: PanePlans, plans: plans, columns: columns, lines: lines, scope: watch.NewScope(plans.Repo),
		now: time.Now, statePath: plan.StatePath(), follow: true}
	if plans.Current == "" {
		a.land()
	}
	a.sequence = mascot.MustAnimation(animation)
	a.frameIndex = 0
	a.hold = a.sequence.Frames[0].Hold
	a.spriteName = a.sequence.Frames[0].Sprite
	a.animated = len(a.sequence.Frames) > 1
	return a
}

func (a *app) play(name string) {
	if a.sequence.Name == name {
		return
	}
	a.sequence = mascot.MustAnimation(name)
	a.frameIndex = 0
	a.hold = a.sequence.Frames[0].Hold
	a.spriteName = a.sequence.Frames[0].Sprite
	a.animated = len(a.sequence.Frames) > 1
}

// syncTurn shows the pose for the current turn, but lets a one-shot
// breakdance finish first.
func (a *app) syncTurn() {
	if a.sequence.Name == "breakdance" {
		return
	}
	a.play(AnimationForTurn(a.turn))
}

func (a *app) frame() string {
	return ComposeFrame(a.spriteName, a.color, a.composeOptions())
}

func (a *app) frameRows() Frame {
	return composeRows(a.spriteName, a.color, a.composeOptions())
}

func (a *app) composeOptions() ComposeOptions {
	opt := ComposeOptions{
		Enabled:     a.enabled,
		Columns:     a.columns,
		Lines:       a.lines,
		Log:         a.log,
		QuitPending: a.quitPending,
		Status:      statusForTurn(a.turn, a.lastKind),
		Cue:         a.unseen[a.other()],
		Scroll:      a.scroll,
		Pane:        a.pane,
		Plans:       a.plans,
		PlanRel:     a.plans.CurrentRel(),
		Follow:      a.followWord(),
		Line:        a.statusLine(),
	}
	if a.composing() {
		opt.Composer = true
		opt.Draft, opt.Placeholder, opt.Preview = a.draft, a.placeholder(), a.preview()
	}
	l := a.layout()
	textW := l.textW
	if textW > 0 {
		opt.LogView = a.logView(textW)
		if len(a.log) == 0 {
			opt.LogView = a.emptyAgent(textW)
		}
	}
	page := a.page()
	switch {
	case a.pane == PaneChat:
		opt.Footer = agentFooter(a.scroll, page != nil, a.offerTakes())
	case a.at == atWait:
		body := a.waitBody(l.textW, l.bodyH)
		a.outline.scroll = max(0, min(a.outline.scroll, len(body)-l.bodyH))
		opt.PlanView, opt.PlanScroll = body, a.outline.scroll
		opt.Footer = waitFooter(len(body) > l.bodyH)
	case a.at != atLibrary && a.doc != nil:
		opt.PlanView, opt.PlanScroll = a.planView(l.textW, l.bodyH)
		opt.Footer = a.planFooter()
		if a.at == atOutline && a.doc.plan != nil && len(opt.PlanView) > l.bodyH {
			// The list scrolls: where the selection is in it.
			rows := outlineRows(a.doc.plan)
			at := 0
			for i, k := range rows {
				if k == a.outline.sel {
					at = i
				}
			}
			// The tag joins the status only while it fits (docs/ux.md §6).
			page.MetaShort = page.Meta
			page.Meta += fmt.Sprintf(" · tasks %d/%d", at, len(rows)-1)
		}
	}
	opt.Page = page
	if a.overlay[a.pane] == overlayHelp {
		body := a.helpBody(textW)
		other := FooterSeg{Text: "tab " + strings.ToLower(tabWords[a.other()])}
		footer := []FooterSeg{{Text: "esc close"}, other}
		if len(body) > l.bodyH {
			footer = []FooterSeg{{Text: "esc close"}, {Text: "↑↓ scroll"}, other}
		}
		opt.Overlay = &Overlay{Lines: body, Footer: footer, Scroll: a.overlayScroll[a.pane]}
		crumbs := []string{tabWords[PaneChat]} // AGENT has no page title of its own under 12 rows
		if page != nil {
			crumbs = page.Crumbs
		}
		opt.Page = &Page{Crumbs: append(append([]string(nil), crumbs...), "HELP")}
	}
	return opt
}

// other is the place not on screen, whose cue the tab bar shows.
func (a *app) other() PaneKind {
	if a.pane == PanePlans {
		return PaneChat
	}
	return PanePlans
}

// light puts a cue on place p's tab while the other place is on screen:
// "!" outranks "•". Showing a place clears its cue, so nothing builds up
// for the place you are looking at, and a cue never moves you (docs/ux.md
// §4).
func (a *app) light(p PaneKind, mark string) {
	if a.pane == p {
		return
	}
	if mark == "!" || a.unseen[p] == "" {
		a.unseen[p] = mark
	}
}

// titled reports that the screen has a page title: PLAN screens and Help
// always, AGENT from agentTitleRows rows (docs/ux.md §4).
func (a *app) titled() bool {
	return a.pane == PanePlans || a.overlay[a.pane] != overlayNone || a.lines >= agentTitleRows
}

// page is the page title of the screen under any overlay (docs/ux.md
// §4), or nil on AGENT without one. Its crumbs are the Esc ladder: Esc
// goes to the crumb on the left.
func (a *app) page() *Page {
	var p Page
	switch {
	case a.pane == PaneChat:
		if a.lines < agentTitleRows {
			return nil
		}
		p = Page{Crumbs: []string{tabWords[PaneChat]}, Meta: "live"}
		switch {
		case len(a.log) == 0:
			p.Meta = "waiting for events"
		case a.scroll > 0 && a.unseenLines > 0:
			p.Meta = fmt.Sprintf("%d↑ · %d new", a.scroll, a.unseenLines)
		case a.scroll > 0:
			p.Meta = fmt.Sprintf("%d↑", a.scroll)
		}
	case a.at == atWait:
		p = Page{Crumbs: []string{"PLANS", "waiting for a plan"}}
	case a.at != atLibrary && a.doc != nil:
		p = a.docPage()
	default:
		p = libraryPage(a.plans)
	}
	return &p
}

// docPage is the open plan's page title: PLANS › its title, then "about"
// or the task's id on a detail screen. On the outline it is the title
// row, selected while the cursor is on it, with the plan's status as meta.
func (a *app) docPage() Page {
	d := a.doc
	crumbs := []string{"PLANS", planTitle(d.plan, d.path)}
	switch {
	case a.at == atPlanDetail:
		return Page{Crumbs: append(crumbs, "about")}
	case a.at == atTaskDetail:
		return Page{Crumbs: append(crumbs, a.outline.sel)}
	case d.plan == nil:
		return Page{Crumbs: crumbs, Meta: "can't read", MetaRole: "error"}
	}
	return Page{Crumbs: crumbs, Meta: string(d.plan.Status), Selected: a.outline.sel == ""}
}

// libraryPage is the plan list's page title: PLANS, then find, the folder
// being browsed (rex/plans/ and each folder under it), or the Name
// prompt, with how many plans the list holds as meta.
func libraryPage(b *PlanBrowser) Page {
	crumbs := []string{"PLANS"}
	if b.Browsing() || (b.Prompting() && b.From == ScreenBrowse) {
		crumbs = append(crumbs, folderCrumbs(b)...)
	}
	switch {
	case b.Screen == ScreenCreate:
		return Page{Crumbs: append(crumbs, "new plan")}
	case b.Screen == ScreenFolder:
		return Page{Crumbs: append(crumbs, "new folder")}
	case b.Browsing():
		return Page{Crumbs: crumbs, Meta: plural(b.total, "%d plan", "%d plans")}
	case b.Find != "":
		return Page{Crumbs: append(crumbs, "find"), Meta: plural(b.total, "%d match", "%d matches")}
	}
	return Page{Crumbs: crumbs, Meta: plural(b.total, "%d plan", "%d plans")}
}

// folderCrumbs is the folder being browsed as crumbs: "rex/plans/", then
// each folder under it.
func folderCrumbs(b *PlanBrowser) []string {
	crumbs := []string{"rex/plans/"}
	if rel, err := filepath.Rel(b.Root, b.Dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			crumbs = append(crumbs, part+"/")
		}
	}
	return crumbs
}

// composing reports that the plan list's composer is on screen and takes
// typing: the only place with one (docs/ux.md §2). Everywhere else keys
// act at once and printable keys do nothing.
func (a *app) composing() bool {
	return a.pane == PanePlans && a.at == atLibrary && a.overlay[PanePlans] == overlayNone
}

// layout is the current screen's layout: without the composer's rows
// where there is none.
func (a *app) layout() layout {
	l := layoutFor(a.columns, a.lines)
	if !a.composing() {
		l = l.bare()
	}
	if !a.titled() {
		l = l.untitled()
	}
	return l
}

// planView lays out the open plan's screen and picks its first visible
// line: the outline keeps the selection in view, details scroll.
func (a *app) planView(width, viewH int) ([]LogLine, int) {
	if width <= 0 {
		return nil, 0
	}
	v := &a.outline
	var body []LogLine
	switch a.at {
	case atPlanDetail:
		body = planDetail(a.doc, width)
	case atTaskDetail:
		body = taskDetail(a.doc, v.sel, width)
	default:
		body = outlineBody(a.doc, v, width)
		head := headOf(body)
		pinned := planHead(body[:head], len(body), viewH)
		if v.sel == "" {
			v.view = 0 // the title row, above the list, is selected: the list starts at its top
		}
		v.view = windowStart(body[head:], viewH-len(pinned), v.view)
		return body, v.view
	}
	v.scroll = max(0, min(v.scroll, len(body)-viewH))
	return body, v.scroll
}

// planFooter is the footer for the open plan's screens (docs/ux.md §4):
// where Esc goes, Tab, what enter does, then ? help. Row 1's offer, while
// enter takes it, leads.
func (a *app) planFooter() []FooterSeg {
	offer := a.offerTakes()
	if a.at == atPlanDetail || a.at == atTaskDetail {
		if offer {
			return []FooterSeg{{Text: "enter switch"}, {Text: "esc outline"}, {Text: "tab agent"}, helpSeg}
		}
		return []FooterSeg{{Text: "esc outline"}, {Text: "tab agent"}, {Text: "↑↓ scroll"}, helpSeg}
	}
	p := a.doc.plan
	switch {
	case offer:
		return []FooterSeg{{Text: "enter switch"}, {Text: "esc plans"}, {Text: "tab agent"}, helpSeg, {Text: "↑↓ move"}}
	case p == nil:
		return []FooterSeg{{Text: "esc plans"}, {Text: "tab agent"}, helpSeg}
	case a.outline.sel == "" && len(p.Tasks) == 0:
		return []FooterSeg{{Text: "esc plans"}, {Text: "tab agent"}, {Text: "enter about"}, helpSeg}
	case a.outline.sel == "":
		return []FooterSeg{{Text: "esc plans"}, {Text: "tab agent"}, {Text: "enter about"}, helpSeg, {Text: "↑↓ move"}}
	}
	return []FooterSeg{{Text: "esc plans"}, {Text: "tab agent"}, {Text: "enter open"}, helpSeg, {Text: "↑↓ move"}}
}

// placeholder is the dim hint in the plan list's empty composer: what you
// can type there.
func (a *app) placeholder() string {
	switch a.plans.Screen {
	case ScreenBrowse:
		return "type to filter"
	case ScreenCreate:
		return "plan name (a/b for a folder)"
	case ScreenFolder:
		return "folder name"
	}
	return "find or create a plan"
}

// preview says what enter will do with a /word in the plan list, for the
// footer; other text there is a find, filter or name.
func (a *app) preview() string {
	text := strings.TrimSpace(a.draft)
	if !forcedVerb(text) {
		return ""
	}
	if verb, ok := parseVerb(text); ok {
		return "enter " + verb
	}
	return "enter not a verb"
}

// statusLine picks row 1, the first of these that applies (docs/ux.md
// §4): the plan file can't be read, a toast, hard drift, a new plan
// (offered or held), soft drift, the pin.
func (a *app) statusLine() StatusLine {
	s := a.statusText()
	if a.doc != nil && s.Text != "" {
		s.Right = progress(a.doc.plan) // while a plan is open, whatever row 1 says
	}
	return s
}

func (a *app) statusText() StatusLine {
	if s, ok := a.overOffer(); ok {
		return s
	}
	if s, ok := a.offerStatus(); ok {
		return s
	}
	if s, ok := a.driftStatus(plan.Soft); ok {
		return s
	}
	if a.doc == nil {
		return StatusLine{}
	}
	return pinStatus(a.doc.plan)
}

// overOffer is what row 1 shows over a new plan: the plan file can't be
// read, a toast (it expires, and the offer is back), hard drift.
func (a *app) overOffer() (StatusLine, bool) {
	if s, ok := docStatus(a.doc); ok {
		return s, true
	}
	if a.toast.Text != "" && a.now().Before(a.toastUntil) {
		return a.toast, true
	}
	return a.driftStatus(plan.Hard)
}

// flash shows a toast on row 1 for a few seconds.
func (a *app) flash(text, kind string) {
	a.toast = StatusLine{Text: text, Kind: kind}
	a.toastUntil = a.now().Add(toastFor)
}

// noPlanText heads the wait screen: the agent writes the plan and Rex
// opens it (docs/ux.md §11).
const noPlanText = "no plan yet · tell your agent to make one"

const toastFor = 3 * time.Second

// exitLine is printed once the screen is restored, so the terminal keeps a
// trace of where you were.
func (a *app) exitLine() string {
	// Printed after the terminal is restored: plan text must not reach it raw.
	clean := func(s string) string { return strings.TrimSpace(strings.ReplaceAll(watch.CleanText(s), "\n", " ")) }
	if rel := clean(a.plans.CurrentRel()); rel != "" {
		if a.doc != nil && a.doc.plan != nil {
			if pin := a.doc.plan.Graph().Pin(); pin != nil && pin.Status == plan.Active {
				return "rex: " + rel + " · pinned " + clean(pin.ID)
			} else if pin != nil {
				return "rex: " + rel + " · next up " + clean(pin.ID)
			}
		}
		return "rex: " + rel
	}
	return "rex: no plan open"
}

func (a *app) logView(width int) []LogLine {
	opt := a.viewOpt()
	if !a.view.ok || a.view.rev != a.logRev || a.view.width != width || a.view.opt != opt {
		a.view = logViewCache{rev: a.logRev, width: width, opt: opt, lines: BuildLogView(a.log, width, opt), ok: true}
	}
	return a.view.lines
}

// viewOpt is how AGENT lays out now (docs/ux.md §10): a thought run that
// is the newest block is live while the turn is in flight, and blocks get
// blank rows from agentTitleRows rows. Live never looks at the scroll or
// at what the turn last did: scrolling and a hidden tool call (a file
// look) leave the layout alone, so one ↑ is one row and nothing bounces.
// A drawn block or the turn's end settles the run.
func (a *app) viewOpt() viewOpt {
	return viewOpt{live: a.turn == TurnThinking || a.turn == TurnRunning, tall: a.lines >= agentTitleRows}
}

func (a *app) logLines() int {
	textW := PaneTextWidth(a.columns)
	if textW <= 0 {
		return 0
	}
	return len(a.logView(textW))
}

// ingest applies hook events: turn state for every event, AGENT for the
// ones worth drawing. A scrolled-up view keeps showing the same lines while
// new ones arrive below, a run under it settles, or MaxLog trims the oldest
// above it (docs/ux.md §10).
func (a *app) ingest(fresh []watch.Event) (changed bool) {
	for _, ev := range fresh {
		a.health.saw(ev)
	}
	hidden := a.hidden
	fresh = a.own(fromFeed(fresh), true)
	// A plan file the agent wrote is opened once the batch is on AGENT
	// and judged for drift against the plan it was written under.
	defer func() {
		if a.planWritten() {
			changed = true
		}
	}()
	if len(fresh) == 0 {
		// A hidden event still changes the count the empty AGENT tab shows.
		return a.hidden != hidden && len(a.log) == 0
	}
	// The layout as it was before the batch: a turn that ends settles the
	// live run, so both the rows and a trim are measured on it.
	opt, before := a.viewOpt(), a.logLines()
	prevTurn := a.turn
	for _, ev := range fresh {
		a.turn = ApplyTurn(a.turn, ev)
		switch {
		case watch.KeepInPane(ev) || ev.Kind == "idle": // a turn end clears an old error
			a.lastKind = ev.Kind
		case ev.Kind == "result" && a.lastKind == "question":
			a.lastKind = "command" // the question was answered: back to work
		}
	}
	n := len(a.log)
	var old []watch.Event // the log as it was, while scrolled: a trim is measured on it
	if a.scroll > 0 {
		old = append(old, a.log...) // a growing reply changes its last event in place
	}
	added, dropped := extendLog(&a.log, fresh)
	if len(added) > 0 || len(a.log) != n {
		a.logRev++ // a trim to MaxLog changes AGENT too
	}
	if dropped > 0 && a.scroll > 0 {
		// The cap takes the oldest lines off the top, which a view counted
		// from the bottom never moves for.
		before -= trimmedRows(old, dropped, PaneTextWidth(a.columns), opt)
	}
	if len(added) > 0 || a.turn != prevTurn {
		a.below(before) // a turn's end draws nothing but can settle a run
	}
	for _, ev := range added {
		// Only what AGENT draws: never a file look, a result or a
		// thought, another repo's event, or Rex's own line (docs/ux.md §4).
		switch {
		case ev.Kind == "text":
			a.light(PaneChat, "•")
		case ev.Kind == "question", ev.Kind == "error" && watch.MetaTool(ev) == "":
			a.light(PaneChat, "!")
		}
	}
	drifted := a.applyDrift() // after the batch, so its block sits below what it judges
	if len(added) > 0 || a.turn != prevTurn {
		a.syncTurn()
		return true
	}
	return drifted
}

// keys applies one read's keys in order, settling after each one, so a
// filter letter and enter read together act like two separate presses.
func (a *app) keys(pressed []string) bool {
	changed := false
	for _, key := range pressed {
		if a.handleKey(key) {
			changed = true
		}
		if a.quit {
			return changed
		}
		if a.settle(a.columns, a.lines) {
			changed = true
		}
	}
	return changed
}

// step applies one read of the loop: its keys first, so each acts on the
// frame you saw, then the events polled with it, so a draft typed in that
// read holds the attach they may bring (docs/ux.md §11).
func (a *app) step(pressed []string, events []watch.Event) bool {
	changed := a.keys(pressed)
	if a.quit {
		return changed
	}
	if a.ingest(events) {
		changed = true
	}
	return changed
}

// feedKinds are the kinds an adapter writes (AGENTS.md, "Kinds").
var feedKinds = map[string]bool{
	"thought": true, "text": true, "user": true, "command": true,
	"question": true, "result": true, "idle": true, "error": true,
}

// fromFeed drops feed lines that claim to be Rex's own blocks (the
// "before rex started" rule, a plan or drift line): those are made in this
// process only.
func fromFeed(events []watch.Event) []watch.Event {
	kept := events[:0:0]
	for _, ev := range events {
		if ev.Source != "rex" && feedKinds[ev.Kind] {
			kept = append(kept, ev)
		}
	}
	return kept
}

// own keeps the events that belong to this repo, with paths shown relative
// to it, and counts the rest. Events with no known origin are kept.
func (a *app) own(events []watch.Event, live bool) []watch.Event {
	var out []watch.Event
	for _, ev := range events {
		if in, known := a.scope.Owns(ev); known && !in {
			a.hidden++
			continue
		}
		if live {
			a.checkDrift(ev) // history from before Rex started never counts
			a.sawWrite(ev)   // nor opens a plan; raw, since quiet results never reach AGENT
		}
		out = append(out, a.scope.Display(ev))
	}
	return out
}

// warmStart shows what this repo's agent was doing just before Rex
// started (docs/ux.md §10): the turn is restored only if the newest event
// is under two minutes old, and the last few events are painted under a
// "before rex started" rule.
func (a *app) warmStart(recent []watch.Event, now time.Time) {
	recent = a.own(fromFeed(recent), false)
	if len(recent) == 0 {
		return
	}
	newest := recent[len(recent)-1].TS
	if now.Sub(time.Unix(0, int64(newest*1e9))) < warmTurnAge {
		for _, ev := range recent {
			a.turn = ApplyTurn(a.turn, ev)
			if watch.KeepInPane(ev) || ev.Kind == "idle" { // as in ingest: a turn end clears an old error
				a.lastKind = ev.Kind
			}
		}
		a.syncTurn()
	}
	// The same merging and dedupe as the live stream, so a growing reply is
	// one block; then the last few blocks.
	var shown []watch.Event
	ExtendLog(&shown, recent)
	if len(shown) > warmEvents {
		shown = shown[len(shown)-warmEvents:]
	}
	if len(shown) == 0 {
		return
	}
	a.log = append([]watch.Event{watch.NewEvent("rex", "before", "before rex started", nil)}, shown...)
	a.logRev++
}

const (
	warmTurnAge = 2 * time.Minute
	warmEvents  = 20
	warmRead    = 200
)

// handleKey applies one key. It sets a.quit on the second Ctrl+C.
func (a *app) handleKey(key string) bool {
	// A Library error stays only until the next key, whatever that key
	// does (docs/ux.md §11); the key may set a new one.
	cleared := a.plans.Err != ""
	a.plans.Err = ""
	return a.applyKey(key) || cleared
}

// helpKey reports that key is ? opening Help at once (docs/ux.md §2): on
// every screen without a composer, and in the plan list while its
// composer is empty. Inside Help it does nothing; after other text it is
// text.
func (a *app) helpKey(key string) bool {
	return key == "?" && a.overlay[a.pane] == overlayNone && (!a.composing() || a.draft == "")
}

func (a *app) applyKey(key string) bool {
	if l := layoutFor(a.columns, a.lines); !l.typing && key != CtrlC && key != "\x0c" {
		_, paste := IsPaste(key)
		// ? opening Help types nothing, so it still works on a short screen.
		edits := paste || (isPrintableKey(key) && !a.helpKey(key)) || key == "\x7f" || key == "\x08" || key == "\x15" || key == "\x17"
		submits := (key == "\r" || key == "\n") && a.draft != "" && a.composing()
		if l.locked || edits || submits {
			// The UI isn't visible (locked), or the composer isn't: nothing
			// may type or act on it blind. A swallowed key still disarms quit.
			armed := a.quitPending
			a.quitPending = false
			return armed
		}
	}
	switch key {
	case "\x0c":
		a.repaint = true
		a.quitPending = false // like any other key, it disarms quit
		return true
	case CtrlC:
		if a.quitPending {
			a.quit = true
			return false
		}
		// The first Ctrl+C closes Help, clears the plan list's draft when
		// you are in PLAN, and arms quit.
		a.overlay[a.pane] = overlayNone
		if a.pane == PanePlans {
			a.draft = ""
		}
		a.quitPending = true
		a.quitTicks = QuitWindowTicks
		return true
	case "\t":
		a.quitPending = false
		if a.pane == PanePlans {
			a.pane = PaneChat
		} else {
			a.pane = PanePlans
			a.plans.Rebuild()
		}
		a.unseen[a.pane] = "" // shown: its news is seen
		return true
	}
	if a.quitPending {
		// Any other key disarms quit; redraw even if the key itself did
		// nothing, or the footer would keep saying "ctrl+c again".
		a.quitPending = false
		a.handleKeyArmed(key)
		return true
	}
	return a.handleKeyArmed(key)
}

// handleKeyArmed applies a key once quit is not armed. Only the plan list
// has a composer; elsewhere a printable key or a paste does nothing, and
// no hidden draft is built.
func (a *app) handleKeyArmed(key string) bool {
	if a.helpKey(key) {
		a.openHelp()
		return true
	}
	text, paste := IsPaste(key)
	if !a.composing() {
		if paste || isPrintableKey(key) {
			return false
		}
	} else if paste {
		// A paste is typed into the draft as one line; it never submits.
		next := capDraft(a.draft + text)
		changed := next != a.draft
		a.draft = next
		return changed
	} else {
		switch key {
		case "\x15":
			a.draft = ""
			return true
		case "\x17":
			a.draft = deleteWord(a.draft)
			return true
		case "\x7f", "\x08":
			next := backspace(a.draft)
			changed := next != a.draft
			a.draft = next
			return changed
		}
		if isPrintableKey(key) {
			if runeLen(a.draft) >= draftCap {
				return false
			}
			a.draft += key
			return true
		}
	}
	if a.overlay[a.pane] != overlayNone {
		return a.overlayKey(key)
	}
	if a.pane == PanePlans {
		return a.planKey(key)
	}
	return a.tapeKey(key)
}

func (a *app) openHelp() {
	a.overlay[a.pane] = overlayHelp
	a.overlayScroll[a.pane] = 0
	a.helpOver[a.pane] = a.pageKind()
}

// submit runs the plan list's draft if it is a /word: /help or
// /breakdance (docs/ux.md §2).
func (a *app) submit(text string) bool {
	verb, ok := parseVerb(text)
	if !ok {
		return false
	}
	a.draft = ""
	switch verb {
	case "help":
		a.openHelp()
	case "breakdance":
		a.play("breakdance")
	}
	return true
}

func (a *app) tapeKey(key string) bool {
	defer a.live()
	bodyH := PaneBodyHeight(a.lines)
	total := a.logLines()
	maxScroll := ClampScroll(total, bodyH, total)
	switch key {
	case "up":
		a.scroll = min(maxScroll, a.scroll+1)
		return true
	case "down":
		a.scroll = max(0, a.scroll-1)
		return true
	case "pageup":
		a.scroll = min(maxScroll, a.scroll+max(1, bodyH))
		return true
	case "pagedown":
		a.scroll = max(0, a.scroll-max(1, bodyH))
		return true
	case "home":
		a.scroll = maxScroll
		return true
	case "end":
		a.scroll = 0
		return true
	case "\r", "\n":
		return a.takeOffer() // PLAN switches; you stay on AGENT
	}
	return false
}

// live keeps the count of lines that came in below a scrolled view to the
// lines still below it, so "N↑ · M new" never has M over N, and clears it
// once the view is back at the bottom.
func (a *app) live() {
	a.unseenLines = min(a.unseenLines, a.scroll)
}

func (a *app) planKey(key string) bool {
	if a.at == atWait {
		return a.waitKey(key)
	}
	if a.at != atLibrary && a.doc != nil {
		return a.docKey(key)
	}
	b := a.plans
	bodyH := a.layout().bodyH
	dir := b.Dir
	defer func() {
		if b.Dir != dir {
			a.draft = "" // a new folder starts unfiltered
		}
	}()
	switch key {
	case "up":
		b.Move(-1)
		return true
	case "down":
		b.Move(1)
		return true
	case "pageup":
		b.Move(-max(1, bodyH))
		return true
	case "pagedown":
		b.Move(max(1, bodyH))
		return true
	case "home":
		// Through Move, so a Name prompt leaves the list's cursor where its
		// Esc comes back to, as ↑↓ do (docs/ux.md §3).
		b.Move(-len(b.Rows))
		return true
	case "end":
		b.Move(len(b.Rows))
		return true
	case "esc":
		// Esc clears first, then climbs one level, and never leaves PLAN.
		if a.draft != "" {
			a.draft = ""
			return true
		}
		return a.planBack()
	case "left":
		if a.draft != "" || b.Prompting() {
			return false
		}
		return a.planBack()
	case "right":
		if b.Browsing() && b.Cursor < len(b.Rows) && b.Rows[b.Cursor].Kind == PlanRowDir {
			b.Activate()
			a.draft = ""
			return true
		}
		return false
	case "\r", "\n":
		if forcedVerb(a.draft) {
			// A /word is only ever a verb: it never names or opens anything.
			if !a.submit(a.draft) {
				b.Err = "'" + strings.TrimSpace(a.draft) + "' is not a verb · try /help"
			}
			return true
		}
		return a.planEnter()
	}
	return false
}

// planBack goes up one level inside PLAN: cancel a name prompt, leave a
// folder, leave Browse for Home, and from Home with no plan open go back
// to the Wait screen (docs/ux.md §3). Otherwise Home is the top.
func (a *app) planBack() bool {
	b := a.plans
	switch {
	case b.Prompting():
		b.CancelPrompt()
		a.draft = ""
		return true
	case b.Browsing():
		b.Up()
		a.draft = ""
		return true
	case b.Current == "":
		a.at = atWait
		return true
	}
	return false
}

func (a *app) planEnter() bool {
	b := a.plans
	switch b.Screen {
	case ScreenCreate:
		held := a.pending
		if b.CommitCreate(a.draft) == PlanOpenChat {
			a.draft = ""
			a.openedPlan()
			a.latch() // you made it: latched (docs/ux.md §11)
			a.offerHeld(held)
		}
		return true
	case ScreenFolder:
		_ = b.CommitFolder(a.draft)
		a.draft = ""
		return true
	}
	filter, was, held := b.Filter, b.Current, a.pending
	switch b.Activate() {
	case PlanStartCreate:
		// From a Browse filter, what was typed becomes the name to edit.
		b.Screen = ScreenCreate
		a.draft = ""
		if b.From == ScreenBrowse {
			a.draft = filter
		}
		b.Name = a.draft
	case PlanStartFolder:
		b.Screen = ScreenFolder
		a.draft = ""
	case PlanStartBrowse:
		a.draft = ""
	case PlanOpenChat:
		a.openedPlan()
		if !samePlan(b.Current, was) {
			a.latch() // you picked it: latched (docs/ux.md §11); Continue picks nothing new
		}
		a.offerHeld(held)
	}
	return true
}

// openedPlan runs after a plan is opened, created or attached: Rex reads
// it, lands PLAN on its outline with the pin selected, and remembers it as
// this repo's last plan.
func (a *app) openedPlan() {
	path := a.plans.Current
	now := a.now()
	if a.doc == nil || !samePlan(a.doc.path, path) {
		a.doc = openDoc(path, now)
		a.outline = outlineView{sel: defaultSel(a.doc.plan)}
		a.toast = StatusLine{} // the last plan's news isn't this one's
	}
	// Continue on the plan already open keeps its last good copy, its
	// debounce and its selection. The plan list's find text is done with
	// (an attach never comes while it holds any, docs/ux.md §11).
	a.draft = ""
	a.at = atOutline
	a.offer, a.pending = "", "" // any open settles which plan is shown
	a.syncDrift()
	_ = plan.UpdateState(a.statePath, now, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: path, Opened: float64(now.Unix())}
	})
}

// docKey handles keys on the open plan's outline and detail screens. They
// have no composer: every key acts at once (docs/ux.md §3).
func (a *app) docKey(key string) bool {
	v := &a.outline
	p := a.doc.plan
	step := max(1, a.layout().bodyH-2)
	if a.at == atOutline {
		switch key {
		case "up", "down", "pageup", "pagedown":
			if p == nil {
				return false
			}
			delta := map[string]int{"up": -1, "down": 1, "pageup": -step, "pagedown": step}[key]
			v.move(p, delta)
			return true
		case "home", "end":
			if p == nil {
				return false
			}
			v.edge(p, key == "end")
			return true
		case "right":
			return a.openDetail()
		}
	} else {
		switch key {
		case "up":
			v.scroll = max(0, v.scroll-1)
			return true
		case "down":
			v.scroll++
			return true
		case "pageup":
			v.scroll = max(0, v.scroll-step)
			return true
		case "pagedown":
			v.scroll += step
			return true
		case "home":
			v.scroll = 0
			return true
		case "end":
			v.scroll = 1 << 30 // clamped when drawn
			return true
		}
	}
	switch key {
	case "esc", "left":
		if a.at == atOutline {
			return a.toLibrary()
		}
		a.at = atOutline
		a.outline.keepSel(a.doc.plan) // the task read in detail may be gone
		return true
	case "\r", "\n":
		if a.takeOffer() {
			return true
		}
		if a.at == atOutline {
			return a.openDetail()
		}
		return false
	}
	return false
}

func (a *app) openDetail() bool {
	if a.doc.plan == nil {
		return false
	}
	a.at = atTaskDetail
	if a.outline.sel == "" {
		a.at = atPlanDetail
	}
	a.outline.scroll = 0
	return true
}

// toLibrary leaves the open plan for Library Home, cursor on Continue. The
// plan stays open: row 0 and AGENT keep it.
func (a *app) toLibrary() bool {
	b := a.plans
	a.at = atLibrary
	a.draft = ""
	b.Screen, b.Find, b.Filter = ScreenHome, "", ""
	b.fresh = true
	b.Rebuild()
	return true
}

// pollDoc re-reads the open plan. An edit keeps the selection, shows a
// toast, and leaves one "plan" line on AGENT. The agent pins
// and finishes tasks this way: when the file marks the pin done, the
// toast says so and names what is next up (docs/ux.md §7, §8).
func (a *app) pollDoc() bool {
	if a.doc == nil {
		return false
	}
	hadTasks := a.doc.plan != nil && len(a.doc.plan.Tasks) > 0
	var was *plan.Task
	if a.doc.plan != nil {
		was = a.doc.plan.Graph().Pin()
	}
	changed, news := a.doc.poll(a.now())
	if !changed {
		return false
	}
	if _, bad := docStatus(a.doc); bad {
		a.light(PanePlans, "!") // the file is gone or doesn't parse
	} else if a.unseen[PanePlans] == "!" {
		a.unseen[PanePlans] = "•" // fixed: nothing needs you, but the plan did change
	}
	if a.at != atTaskDetail {
		a.outline.keepSel(a.doc.plan)
	}
	if p := a.doc.plan; !hadTasks && p != nil && len(p.Tasks) > 0 && a.outline.sel == "" {
		// An agent just filled an empty plan: land on its pin, as an open does.
		a.outline.sel = defaultSel(p)
	}
	a.rejudgeDrift()
	if news.text == "" {
		return true
	}
	a.tapeLine("plan", news.text)
	a.light(PanePlans, "•")
	if done := doneToast(was, a.doc.plan); done.Text != "" {
		a.toast, a.toastUntil = done, a.now().Add(toastFor)
		return true
	}
	if !news.tasks && strings.HasPrefix(a.toast.Text, "✓ ") && a.now().Before(a.toastUntil) {
		// The agent finishes a task in several saves (its retrospective,
		// the plan's own status): the done news stays (docs/ux.md §8).
		return true
	}
	a.flash("plan updated · "+news.text, "accent")
	return true
}

// reloadDoc re-reads the open plan now, without waiting for the next poll.
func (a *app) reloadDoc() bool {
	if a.doc == nil {
		return false
	}
	a.doc.polled = time.Time{}
	return a.pollDoc()
}

// doneToast is row 1's news when the file marks the old pin done: what is
// next up (or now, when the same save started it), or that the plan is
// done; zero when the pin wasn't finished.
// The plan is done when every leaf is, as the pin line judges it: an
// agent may save the leaf and the plan's own status in two writes.
func doneToast(was *plan.Task, p *plan.Plan) StatusLine {
	if was == nil || was.ID == "" || p == nil {
		return StatusLine{}
	}
	g := p.Graph()
	if t := g.Task(was.ID); t == nil || t.Status != plan.Done {
		return StatusLine{}
	}
	tail, short := " · nothing else is unblocked", " · nothing unblocked"
	if next := g.Pin(); next != nil && next.Status == plan.Active {
		tail, short = " · now "+next.ID, " · now "+next.ID // the same save started it
	} else if next != nil {
		tail, short = " · next up "+next.ID, " · next "+next.ID
	} else if done, total := g.Progress(); done == total {
		tail, short = " · the plan is done", " · plan done"
	}
	id := was.ID
	return StatusLine{Text: "✓ " + id + " done" + tail, Kind: "accent", Fit: func(w int) string { return fitDone(id, tail, short, w) }}
}

// fitDone fits the done toast to width cells. What is next matters more
// than what just finished, so the done id clips from the left first
// ("✓ …ghlight done · next up load-outline"), down to headPathMin cells,
// then the tail shortens.
func fitDone(id, tail, short string, width int) string {
	for _, t := range []string{tail, short} {
		room := width - cellLen("✓  done"+t)
		if cellLen(id) <= room {
			return "✓ " + id + " done" + t
		}
		if room >= headPathMin {
			return "✓ …" + tailCells(id, room-1) + " done" + t
		}
	}
	return "✓ done" + short
}

// overlayKey handles keys while Help covers the current place: Esc closes
// it, the arrows and pages scroll it, and nothing else does anything.
func (a *app) overlayKey(key string) bool {
	switch key {
	case "esc":
		a.overlay[a.pane] = overlayNone
		return true
	case "up", "down", "pageup", "pagedown":
		page := a.layout().bodyH
		// A page keeps two rows of overlap: the ↑/↓ more markers cover them.
		step := max(1, page-2)
		delta := map[string]int{"up": -1, "down": 1, "pageup": -step, "pagedown": step}[key]
		limit := max(0, len(a.helpBody(a.layout().textW))-page)
		next := max(0, min(limit, a.overlayScroll[a.pane]+delta))
		changed := next != a.overlayScroll[a.pane]
		a.overlayScroll[a.pane] = next
		return changed
	}
	return false
}

// helpBody is the Help overlay's lines at width: the page it was opened
// over first, then the feed's health (docs/ux.md §13).
func (a *app) helpBody(width int) []LogLine {
	tall := a.lines >= 16
	help := helpLines(width, a.helpOver[a.pane], tall)
	if tall {
		help = append(help, LogLine{Kind: "gap"})
	}
	a.health.refresh(time.Now())
	return append(help, a.health.lines(width, a.hidden, a.skill.look(a.plans.Repo, a.now()), time.Now(), false)...)
}

// pageKind names the page Help opens over, for its THIS PAGE section.
func (a *app) pageKind() string {
	switch {
	case a.pane == PaneChat:
		return "agent"
	case a.at == atWait:
		return "waiting"
	case a.at == atLibrary || a.doc == nil:
	case a.at == atPlanDetail:
		return "about"
	case a.at == atTaskDetail:
		return "task"
	default:
		return "outline"
	}
	switch a.plans.Screen {
	case ScreenBrowse:
		return "folder"
	case ScreenCreate:
		return "new plan"
	case ScreenFolder:
		return "new folder"
	}
	return "plans"
}

// tapeLine adds one local line to AGENT. It is never written to the
// feed and never sent anywhere.
func (a *app) tapeLine(kind, text string) {
	before := a.logLines()
	a.log = append(a.log, watch.NewEvent("rex", kind, text, nil))
	a.logRev++
	a.below(before)
}

// below keeps a scrolled-up view on the lines it showed while the stream
// went from before lines, and counts the new ones for AGENT's page title.
// The change is signed (a block can take fewer rows than it did), and a
// scrolled view stays scrolled (docs/ux.md §10).
func (a *app) below(before int) {
	if a.scroll > 0 {
		grew := a.logLines() - before
		a.scroll = max(1, a.scroll+grew)
		a.unseenLines = max(0, a.unseenLines+grew)
	}
}

// syncPlanInput hands the plan draft to the screen that reads it: Browse
// filters, Home finds, the name prompt previews where it will create. A
// new query puts the cursor on its first match.
func (a *app) syncPlanInput() bool {
	b := a.plans
	if a.at != atLibrary {
		return false
	}
	text := a.draft
	if forcedVerb(text) {
		text = ""
	}
	// Spaces alone don't filter or find anything, and on Home neither does
	// text that names no plan (".rex.json", "./").
	text = strings.TrimSpace(text)
	if b.Screen == ScreenHome && findKey(text) == "" {
		text = ""
	}
	switch b.Screen {
	case ScreenBrowse:
		if b.Filter == text {
			return false
		}
		b.Filter = text
	case ScreenHome:
		if b.Find == text {
			return false
		}
		b.Find = text
	case ScreenCreate, ScreenFolder:
		if b.Name == a.draft {
			return false
		}
		b.Name = a.draft
		return true
	default:
		return false
	}
	b.fresh = true
	b.Rebuild()
	return true
}

// tick runs on the frame clock: the quit window counts down and the
// mascot advances a frame.
func (a *app) tick() bool {
	changed := a.pollDoc()
	if a.scanPlans() {
		changed = true
	}
	if a.toast.Text != "" && !a.now().Before(a.toastUntil) {
		a.toast = StatusLine{}
		changed = true
	}
	if a.quitPending {
		a.quitTicks--
		if a.quitTicks <= 0 {
			a.quitPending = false
			changed = true
		}
	}
	if !a.animated {
		return changed
	}
	var finished bool
	a.frameIndex, a.hold, finished = advance(a.sequence.Frames, a.frameIndex, a.hold, animationLoops(a.sequence.Name))
	if finished {
		a.play(AnimationForTurn(a.turn))
		return true
	}
	if next := a.sequence.Frames[a.frameIndex].Sprite; next != a.spriteName {
		a.spriteName = next
		changed = true
	}
	return changed
}

// settle runs after every key or tick: it applies the browse filter, opens
// a held attach, picks up a resize, and keeps AGENT's scroll in range.
func (a *app) settle(columns, lines int) bool {
	changed := false
	if a.composing() && a.syncPlanInput() {
		changed = true
	}
	if a.applyPending() {
		changed = true
	}
	if columns != a.columns || lines != a.lines {
		a.columns, a.lines = columns, lines
		a.repaint = true // a resize repaints everything, whatever the frame shape
		changed = true
	}
	total := a.logLines()
	if clamped := ClampScroll(total, PaneBodyHeight(a.lines), a.scroll); clamped != a.scroll {
		a.scroll = clamped
		changed = true
	}
	a.live()
	return changed
}

const draftCap = 256

// capDraft keeps the composer to draftCap whole runes.
func capDraft(s string) string {
	if r := []rune(s); len(r) > draftCap {
		return string(r[:draftCap])
	}
	return s
}
