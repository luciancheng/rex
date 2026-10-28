package tui

import (
	"fmt"
	"os"
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
	glance     int // frame ticks left of the glance a plan's open plays (docs/ux.md §12)

	columns, lines int

	pane  PaneKind
	plans *PlanBrowser
	draft string // the text in Plans' field: the only text Rex takes (docs/ux.md §2)

	overlay       [2]overlayKind // per place
	overlayScroll [2]int
	// helpOver names the page each place's Help was opened over, for THIS
	// PAGE (docs/ux.md §13): a plan that opens under Help doesn't swap it
	// mid-read. Help's crumbs are the page under it now, the Esc ladder.
	helpOver [2]string
	// needs is your agent asking ("ask") or its turn's error ("error"),
	// seen on PLAN's notice row until you look at AGENT (docs/ux.md §4).
	// Memory only.
	needs string

	log         []watch.Event
	logRev      int
	view        logViewCache
	scroll      int
	emptyScroll int // rows of the empty AGENT tab above the screen: a page, read from its top
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
	at         planAt   // which PLAN screen: Plans, the task list or a page
	outline    outlineView
	pages      pageCache // the task details or About laid out, for the frames that only scroll it
	pageOver   bool      // the open plan's page with no list runs past the screen: ↑↓ scroll it
	toast      StatusLine
	toastUntil time.Time
	toastWait  bool       // raised while AGENT was on screen: its time starts on PLAN
	toastDone  bool       // the toast is done news, which a save that moves no task keeps up
	unsaid     *docChange // a change read from the open plan, waiting to be credited (docs/ux.md §7b)
	mine       *ownEdit   // Rex's last write to it, until the next reload says whether it held
	late       *lateClaim // a change announced as the plan's that a late hook may still claim
	now        func() time.Time
	statePath  string
	drift      driftState    // drift from the current pin, shown only
	driftGen   int           // numbers each pin stint's drift block
	driftQueue []watch.Event // live events to judge once their batch is on AGENT
	skill      skillSeen     // whether an agent can learn plans, for Plans' first run

	// Attach (docs/ux.md §11): follow opens the plan the agent writes;
	// any pick on Plans but Continue and Open latest latches (follow
	// false), and another plan's write is then only offered on the notice
	// row.
	follow      bool
	pending     string    // a write to open once Plans' field clears, or you are back on the task list
	offer       string    // latched: the plan the agent wrote, o opens it
	pendingName wroteAs   // how the notice row names pending, read once when it is set
	offerName   wroteAs   // and offer
	wrote       string    // the last other plan file written in the batch being ingested
	wroteOpen   bool      // the batch wrote the open plan too
	scanned     time.Time // the last refresh of Plans' cards from the folder
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
	plans.now = func() time.Time { return a.now() } // Open latest's "changed 2m ago"
	a.land()
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

// glanceTicks is how long the glance holds: as long as the idle cycle's.
const glanceTicks = 14

// glanceOnce has the mascot glance aside once (docs/ux.md §12): a plan
// opened. Only where the mascot shows, and never over a breakdance, which
// plays whole. The pose under it keeps its clock.
func (a *app) glanceOnce() {
	if a.layout().mascotCols > 0 && a.sequence.Name != "breakdance" {
		a.glance = glanceTicks
	}
}

// sprite is the pose drawn now: the glance while it holds, else the
// animation's frame.
func (a *app) sprite() string {
	if a.glance > 0 {
		return mascot.Glance
	}
	return a.spriteName
}

func (a *app) frame() string {
	return ComposeFrame(a.sprite(), a.color, a.composeOptions())
}

func (a *app) frameRows() Frame {
	return composeRows(a.sprite(), a.color, a.composeOptions())
}

func (a *app) composeOptions() ComposeOptions {
	opt := ComposeOptions{
		Enabled:     a.enabled,
		Columns:     a.columns,
		Lines:       a.lines,
		Log:         a.log,
		QuitPending: a.quitPending,
		Status:      statusForTurn(a.turn, a.lastKind),
		Scroll:      a.scroll,
		Pane:        a.pane,
		Context:     a.context(),
		Notice:      a.notice(),
	}
	if a.composing() {
		opt.Composer = a.fieldOpen()
		opt.Draft, opt.Preview = a.draft, a.preview()
	}
	l := a.layout()
	textW := l.textW
	if textW > 0 {
		opt.LogView = a.logView(textW)
		if len(a.log) == 0 {
			opt.LogView = a.emptyAgent(textW)
		}
	}
	switch {
	case a.pane == PaneChat && len(a.log) == 0:
		// The empty tab is a page (docs/ux.md §10): it scrolls from its
		// top.
		a.emptyScroll = max(0, min(a.emptyScroll, len(opt.LogView)-l.bodyH))
		opt.Scroll = a.emptyScroll
		opt.Footer = pageFooter(len(opt.LogView) > l.bodyH, "", "tab plan", true)
	case a.pane == PaneChat:
		opt.Footer = agentFooter()
	case a.at != atLibrary && a.doc != nil:
		opt.PlanRows = a.planRows()
		var above int
		opt.PlanView, above = a.planView(l.textW, opt.PlanRows)
		if above > 0 {
			opt.More = fmt.Sprintf("↑ %d more", above)
		}
		opt.Footer = a.planFooter()
	default:
		// The screen decides what PLAN draws, never an empty view. Open
		// latest says which plan your agent just wrote, and a first run
		// whether your agent can learn the plan format.
		opt.Plans = a.plans
		a.plans.offer = a.offer
		if a.plans.total == 0 {
			a.plans.skill = a.skill.look(a.plans.Repo, a.now())
		}
	}
	opt.Page = a.page()
	if a.overlay[a.pane] == overlayHelp {
		opt.More = ""
		body := a.helpBody(textW)
		footer := pageFooter(len(body) > l.bodyH, "esc close", "tab "+otherPlace(a.pane), false)
		opt.Overlay = &Overlay{Lines: body, Footer: footer, Scroll: a.overlayScroll[a.pane]}
		opt.Page = &Page{Crumbs: append(append([]string(nil), opt.Page.Crumbs...), "Help")}
		opt.Context = StatusLine{Text: "Keys for " + helpPages[a.helpOver[a.pane]] + ", then everywhere.", Kind: "muted"}
	}
	return opt
}

// helpPages names the page Help was opened over, on Help's row 1.
var helpPages = map[string]string{
	"agent": "Agent", "about": "About", "task": "this task", "tasks": "the task list",
	"no tasks": "a plan with no tasks", "can't read": "a plan Rex can't read",
	"open a plan": "Open a plan", "new plan": "a new plan", "plans": "Plans",
}

// page is row 0's title for the screen under any overlay (docs/ux.md §4).
// Its crumbs are the Esc ladder: Esc goes to the crumb on the left.
func (a *app) page() *Page {
	var p Page
	switch {
	case a.pane == PaneChat:
		p = Page{Crumbs: []string{"Agent"}, Meta: metaSpans("live")}
		switch {
		case len(a.log) == 0:
			p.Meta = metaSpans("no events yet")
		case a.scroll > 0:
			p.Meta = metaSpans(fmt.Sprintf("↓ %d newer", a.scroll))
		}
	case a.at != atLibrary && a.doc != nil:
		p = a.docPage()
	default:
		p = libraryPage(a.plans)
	}
	return &p
}

// docPage is the open plan's title (docs/ux.md §4): its title, then
// "About" or the task's id on a page of their own. On the task list its
// meta is the plan's progress and how it got here (following, read-only,
// can't read); on details, the task's state.
func (a *app) docPage() Page {
	d := a.doc
	title := planTitle(d.plan, d.path)
	switch {
	case d.plan == nil:
		return Page{Crumbs: []string{title}, Meta: []Span{{Text: "can't read", Role: "error"}}}
	case a.at == atPlanDetail:
		meta := "no tasks"
		if short := progress(d.plan); short != "" {
			meta = short + " done"
		}
		if d.plan.Status != "" {
			meta = string(d.plan.Status) + " · " + meta
		}
		return Page{Crumbs: []string{title, "About"}, Meta: metaSpans(meta)}
	case a.at == atTaskDetail:
		p := Page{Crumbs: []string{title, a.outline.sel}}
		if t := findTask(d.plan, a.outline.sel); t != nil {
			g := d.plan.Graph()
			word, role := taskState(t, g, g.Pin())
			p.Meta = []Span{{Text: word, Role: role}}
		}
		return p
	}
	short := progress(d.plan)
	full := short + " done"
	if short == "" {
		short, full = "no tasks", "no tasks"
	}
	meta := []Span{{Text: full, Role: "muted"}}
	if a.follow {
		meta = append(meta, Span{Text: " · following", Role: "muted"})
	}
	if d.plan.ReadOnly != "" {
		meta = append(meta, Span{Text: " · read-only", Role: "muted"})
	}
	if _, bad := docStatus(d); bad {
		meta = append(meta, Span{Text: " · ", Role: "muted"}, Span{Text: "can't read", Role: "error"})
	}
	return Page{Crumbs: []string{title}, Meta: meta, Short: metaSpans(short)}
}

// libraryPage is Plans' title (docs/ux.md §4): Plans, then Find, Open a
// plan and the folder being browsed (each folder from rex/plans down), or
// New plan, with what the page holds as meta: the repo and its plans, the
// matches, the folder, or where a new plan goes.
func libraryPage(b *PlanBrowser) Page {
	crumbs := []string{"Plans"}
	switch {
	case b.Prompting():
		return Page{Crumbs: append(crumbs, "New plan"), Meta: metaSpans(b.RelDir() + "/")}
	case b.Browsing():
		return Page{Crumbs: append(append(crumbs, "Open a plan"), folderCrumbs(b)...), Meta: metaSpans(b.RelDir() + "/")}
	case b.Find != "":
		return Page{Crumbs: append(crumbs, "Find"), Meta: metaSpans(plural(b.total, "%d match", "%d matches"))}
	}
	p := Page{Crumbs: crumbs}
	p.Meta, p.Short = b.where()
	return p
}

// folderCrumbs is the folder being browsed as crumbs, one per folder
// under rex/plans: Open a plan is its root (docs/ux.md §4).
func folderCrumbs(b *PlanBrowser) []string {
	var crumbs []string
	if rel, err := filepath.Rel(b.Root, b.Dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			crumbs = append(crumbs, part+"/")
		}
	}
	return crumbs
}

// composing reports that Plans is on screen and takes typing into its
// text field: the only place that does (docs/ux.md §2). Everywhere else
// keys act at once and printable keys do nothing.
func (a *app) composing() bool {
	return a.pane == PanePlans && a.at == atLibrary && a.overlay[PanePlans] == overlayNone
}

// fieldOpen reports that Plans' text field shows: Find or the filter with
// text in it, or New plan (docs/ux.md §2).
func (a *app) fieldOpen() bool {
	return a.composing() && (a.draft != "" || a.plans.Prompting())
}

// layout is the current screen's layout.
func (a *app) layout() layout {
	return layoutFor(a.columns, a.lines)
}

// spacedPlan is the open plan's body at blankFrom rows, where row 2 and the
// notice row take two of the rows an 11-row terminal gives it.
var spacedPlan = layoutFor(TextOnlyBelow, blankFrom).bodyH

// planRows is the body rows the open plan's screens are laid out in
// (docs/ux.md §12): 11 rows lay them out as 12 do, so a taller terminal
// never shows less of the task list or a page; the row left over is blank.
func (a *app) planRows() int {
	rows := a.layout().bodyH
	if a.lines < blankFrom {
		rows = min(rows, spacedPlan)
	}
	return rows
}

// pageCache keeps a page of the open plan laid out (task details, About)
// for one read of the plan, task and width: a frame that only scrolls it
// costs the rows it shows, however long the page is.
type pageCache struct {
	plan  *plan.Plan
	at    planAt
	key   string
	width int
	lines []LogLine
}

// planView lays out the open plan's screen in viewH rows (docs/ux.md
// §6): the task list's window around the selection, or a page from its
// scroll. It says how many lines are above what it shows, for row 2's
// ↑ N more; under blankFrom rows a marker takes a row of the body
// instead, and the list has no group labels.
func (a *app) planView(width, viewH int) ([]LogLine, int) {
	if width <= 0 {
		return nil, 0
	}
	v := &a.outline
	roomy := a.lines >= blankFrom
	a.pageOver = false
	if a.at != atPlanDetail && a.at != atTaskDetail {
		if page := listless(a.doc, a.planLabel(a.doc.path), a.plans.CurrentRel(), width); page != nil {
			a.pageOver = len(page) > viewH
			return pageWindow(page, &v.scroll, viewH, roomy)
		}
		return outlineScreen(a.doc.plan, v, width, viewH, roomy)
	}
	c := &a.pages
	if c.plan == nil || c.plan != a.doc.plan || c.at != a.at || c.key != v.sel || c.width != width {
		*c = pageCache{plan: a.doc.plan, at: a.at, key: v.sel, width: width}
		if a.at == atPlanDetail {
			c.lines = aboutPage(a.doc, width)
		} else {
			c.lines = taskPage(a.doc, a.plans.CurrentRel(), v.sel, width)
		}
	}
	return pageWindow(c.lines, &v.scroll, viewH, roomy)
}

// planFooter is the footer for the open plan's screens (docs/ux.md §4):
// on the task list ↑↓, enter details, p and d (where they can write), i
// about, where esc goes, tab, ? help; a page scrolls and esc goes back to
// the tasks, a task's details with p and d too. An offer is taken with o,
// which its notice names.
func (a *app) planFooter() []FooterSeg {
	tab := FooterSeg{Text: "tab agent"}
	switch a.at {
	case atPlanDetail:
		return []FooterSeg{{Text: "↑↓ scroll"}, {Text: "esc tasks"}, tab, helpSeg}
	case atTaskDetail:
		return append(append([]FooterSeg{{Text: "↑↓ scroll"}}, a.statusSegs()...), FooterSeg{Text: "esc tasks"}, tab, helpSeg)
	}
	p := a.doc.plan
	var scroll []FooterSeg
	if a.pageOver {
		scroll = []FooterSeg{{Text: "↑↓ scroll"}}
	}
	switch {
	case p == nil:
		return append(scroll, FooterSeg{Text: "esc plans"}, tab, helpSeg)
	case len(p.Tasks) == 0:
		return append(scroll, FooterSeg{Text: "i about"}, FooterSeg{Text: "esc plans"}, tab, helpSeg)
	}
	out := append([]FooterSeg{{Text: "↑↓ select"}, {Text: "enter details"}}, a.statusSegs()...)
	return append(out, FooterSeg{Text: "i about"}, FooterSeg{Text: "esc plans"}, tab, helpSeg)
}

// preview says what enter will do with a /word on Plans, for the footer;
// other text there is a find, filter or name.
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

// context is row 1 (docs/ux.md §4): on the task list the pin line, on
// AGENT the turn and the pin, elsewhere what the page is for.
func (a *app) context() StatusLine {
	muted := func(text string) StatusLine { return StatusLine{Text: text, Kind: "muted"} }
	b := a.plans
	switch {
	case a.pane == PaneChat:
		return a.turnLine()
	case a.at != atLibrary && a.doc != nil:
		d := a.doc
		switch {
		case d.plan == nil:
			return muted(b.CurrentRel())
		case a.at == atPlanDetail:
			return muted(b.CurrentRel())
		case a.at == atTaskDetail:
			return muted(taskPlace(d.plan, a.outline.sel))
		}
		return pinStatus(d.plan)
	case b.Screen == ScreenCreate:
		return muted("Name it. a/b puts plan b in folder a.")
	case b.Browsing():
		return muted("Folders, then plans. Type to filter.")
	case b.Find != "":
		return muted(`Plans whose name or title has "` + flat(b.Find) + `".`)
	case b.Notice != "":
		// Where plans go, or that they are off, outranks what Plans is for.
		return noticeLine(b.Notice)
	case b.total == 0:
		return muted("No plans here yet.")
	}
	return muted("Pick up where you left off, or start a new plan.")
}

// noticeLine is Plans' notice (outside git, an old rex-plans/, plans off)
// as row 1 draws it, fitted to the row.
func noticeLine(n string) StatusLine {
	if n == "" {
		return StatusLine{}
	}
	return StatusLine{Text: n, Kind: "muted", Fit: func(w int) string { return fitNotice(n, w) }}
}

// turnLine is AGENT's row 1 (docs/ux.md §4): what the turn is doing, and
// on the right the pin the agent gets, or that nothing is pinned.
func (a *app) turnLine() StatusLine {
	if len(a.log) == 0 {
		return StatusLine{Text: feedLead, Short: feedShort, Kind: "muted"}
	}
	s := StatusLine{Text: statusForTurn(a.turn, a.lastKind), Kind: "muted"}
	switch s.Text {
	case "ask":
		s.Text, s.Kind = "waiting on you", "text"
	case "error":
		s.Text, s.Kind = "turn error", "error"
	}
	if a.doc != nil && a.doc.plan != nil {
		s.Right = []Span{{Text: "nothing pinned", Role: "muted"}}
		if pin := a.doc.plan.Graph().Pin(); pin != nil && pin.Status == plan.Active {
			s.Right = []Span{{Text: "pinned ", Role: "muted"}, {Text: flat(pin.ID), Role: "accent"}}
		}
	}
	return s
}

// noticeKind is what the notice row shows, highest first (docs/ux.md §4).
type noticeKind int

const (
	noticeNone noticeKind = iota
	noticeError
	noticeNeeds
	noticeToast
	noticeOffer
	noticeDrift
	noticeReadOnly
)

// notice is the notice row (docs/ux.md §4), or row 1 under blankFrom rows.
func (a *app) notice() StatusLine {
	s, _ := a.noticeOf()
	return s
}

// noticeOf picks the one message the notice row shows, the first of these
// that applies: an error (the plan file can't be read, Plans'
// error, a failed open); your agent asking or its turn's error, on PLAN
// until you look at AGENT; a toast, on PLAN (one raised while AGENT is on
// screen waits for PLAN); a new plan, offered or held; hard drift, on
// PLAN; a read-only plan, on its own screens.
func (a *app) noticeOf() (StatusLine, noticeKind) {
	plans := a.pane == PanePlans
	toast := a.toastLive()
	if s, ok := docStatus(a.doc); ok {
		return errorNotice(s), noticeError
	}
	if plans && a.plans.Err != "" && a.at == atLibrary {
		return errorNotice(StatusLine{Text: a.plans.Err, Kind: "error"}), noticeError
	}
	if toast && a.toast.Kind == "error" {
		return errorNotice(a.toast), noticeError
	}
	if plans && a.needs != "" {
		if a.needs == "error" {
			return errorNotice(StatusLine{Text: "Your agent's turn ended with an error · tab agent", Short: "turn error · tab agent", Kind: "error"}), noticeNeeds
		}
		return StatusLine{Text: "Your agent is asking you something · tab agent to read it", Short: "Your agent asks · tab agent", Kind: "text"}, noticeNeeds
	}
	if plans && toast {
		return a.toast, noticeToast
	}
	if s, ok := a.offerStatus(); ok {
		return s, noticeOffer
	}
	if !plans {
		return StatusLine{}, noticeNone
	}
	if s, ok := a.driftStatus(); ok {
		return s, noticeDrift
	}
	if d := a.doc; d != nil && d.plan != nil && d.plan.ReadOnly != "" && a.at != atLibrary {
		return StatusLine{Text: "Read-only: " + d.plan.ReadOnly + " · p and d are off", Kind: "muted"}, noticeReadOnly
	}
	return StatusLine{}, noticeNone
}

// errorNotice is an error on the notice row: "✗ " and the error, red.
func errorNotice(s StatusLine) StatusLine {
	const mark = "✗ "
	out := StatusLine{Text: mark + s.Text, Kind: "error"}
	if s.Short != "" {
		out.Short = mark + s.Short
	}
	if s.Fit != nil {
		out.Fit = func(w int) string { return mark + s.Fit(w-cellLen(mark)) }
	}
	return out
}

// toastLive reports a toast that still shows: its time isn't up, or it
// waits for PLAN.
func (a *app) toastLive() bool {
	return a.toast.Text != "" && (a.toastWait || a.now().Before(a.toastUntil))
}

// flash shows a toast on the notice row for a few seconds; one raised
// while AGENT is on screen waits for PLAN, an error's shows at once.
func (a *app) flash(text, kind string) {
	a.show(StatusLine{Text: text, Kind: kind})
}

// show puts s up as the toast.
func (a *app) show(s StatusLine) {
	a.toast, a.toastDone = s, false
	a.toastUntil = a.now().Add(toastFor)
	a.toastWait = a.pane != PanePlans && s.Kind != "error"
}

// sawPlan runs when PLAN comes on screen: a waiting toast starts its time.
func (a *app) sawPlan() {
	if a.toastWait {
		a.toastWait = false
		a.toastUntil = a.now().Add(toastFor)
	}
}

// sawAgent runs when AGENT comes on screen: your agent's question or its
// turn's error is seen, and so is hard drift, which AGENT lists.
func (a *app) sawAgent() {
	a.needs = ""
	a.drift.hard = nil
}

const toastFor = 4 * time.Second

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
// blank rows from blankFrom rows. Live never looks at the scroll or
// at what the turn last did: scrolling and a hidden tool call (a file
// look) leave the layout alone, so one ↑ is one row and nothing bounces.
// A drawn block or the turn's end settles the run.
func (a *app) viewOpt() viewOpt {
	return viewOpt{live: a.turn == TurnThinking || a.turn == TurnRunning, tall: a.lines >= blankFrom}
}

func (a *app) logLines() int {
	textW := PaneTextWidth(a.columns, a.lines)
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
	n := len(a.log)
	var old []watch.Event // the log as it was, while scrolled: a trim is measured on it
	if a.scroll > 0 {
		old = append(old, a.log...) // a growing reply changes its last event in place
	}
	// One event at a time, in order, so what PLAN's notice says your agent
	// needs follows the turn as it went, even within one batch.
	var added []watch.Event
	dropped := 0
	for _, ev := range fresh {
		a.turn = ApplyTurn(a.turn, ev)
		switch {
		case watch.KeepInPane(ev) || ev.Kind == "idle": // a turn end clears an old error
			a.lastKind = ev.Kind
		case ev.Kind == "result" && a.lastKind == "question":
			a.lastKind = "command" // the question was answered: back to work
		}
		got, d := extendLog(&a.log, []watch.Event{ev})
		added, dropped = append(added, got...), dropped+d
		a.heard(ev, len(got) > 0)
	}
	if len(added) > 0 || len(a.log) != n {
		a.logRev++ // a trim to MaxLog changes AGENT too
	}
	if dropped > 0 && a.scroll > 0 {
		// The cap takes the oldest lines off the top, which a view counted
		// from the bottom never moves for.
		before -= trimmedRows(old, dropped, PaneTextWidth(a.columns, a.lines), opt)
	}
	if len(added) > 0 || a.turn != prevTurn {
		a.below(before) // a turn's end draws nothing but can settle a run
	}
	drifted := a.applyDrift() // after the batch, so its block sits below what it judges
	if len(added) > 0 || a.turn != prevTurn {
		a.syncTurn()
		return true
	}
	return drifted
}

// heard keeps what PLAN's notice says your agent needs (docs/ux.md §4)
// up to date with one event of this repo's agent, drawn on AGENT or not.
// Only your agent asking, or its turn's error, reaches PLAN: never a
// reply, a failed tool call, another repo's event or Rex's own line. An
// ask holds until the turn goes on past it, as row 1's turn word does (a
// command, a result, a reply, the turn's end, the next prompt); a turn's
// error until the next prompt. Looking at AGENT clears both (sawAgent),
// and nothing is raised while it is on screen.
func (a *app) heard(ev watch.Event, drawn bool) {
	switch {
	case ev.Kind == "user":
		a.needs = ""
	case a.needs == "ask" && a.lastKind != "question":
		a.needs = ""
	}
	if !drawn || a.pane == PaneChat {
		return
	}
	switch {
	case ev.Kind == "question":
		a.needs = "ask"
	case ev.Kind == "error" && watch.MetaTool(ev) == "":
		a.needs = "error"
	}
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
	// Plans' error stays only until the next key, whatever that key does
	// (docs/ux.md §11); the key may set a new one.
	cleared := a.plans.Err != ""
	a.plans.Err = ""
	return a.applyKey(key) || cleared
}

// helpKey reports that key is ? opening Help at once (docs/ux.md §2): on
// every screen without a text field, and on Plans while its field is
// empty. Inside Help it does nothing; after other text it is text.
func (a *app) helpKey(key string) bool {
	return key == "?" && a.overlay[a.pane] == overlayNone && (!a.composing() || a.draft == "")
}

func (a *app) applyKey(key string) bool {
	if l := layoutFor(a.columns, a.lines); !l.typing && key != CtrlC && key != "\x0c" {
		_, paste := IsPaste(key)
		// ? opening Help, o opening an offer, i opening About, and p and d
		// writing a task's status type nothing, so they still work on a
		// short screen.
		edits := paste || (isPrintableKey(key) && !a.helpKey(key) && !a.offerKey(key) && !a.aboutKey(key) && !a.statusKey(key)) || key == "\x7f" || key == "\x08" || key == "\x15" || key == "\x17"
		submits := (key == "\r" || key == "\n") && a.draft != "" && a.composing()
		if l.locked || edits || submits {
			// The UI isn't visible (locked), or the text field isn't: nothing
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
		// The first Ctrl+C closes Help, clears Plans' field when you are in
		// PLAN, and arms quit.
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
			a.sawAgent()
		} else {
			a.pane = PanePlans
			a.plans.Rebuild()
			a.sawPlan()
		}
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

// handleKeyArmed applies a key once quit is not armed. Only Plans takes
// typing, into its text field; elsewhere a printable key or a paste does
// nothing, and no hidden draft is built.
func (a *app) handleKeyArmed(key string) bool {
	if a.helpKey(key) {
		a.openHelp()
		return true
	}
	if a.offerKey(key) {
		return a.takeOffer()
	}
	if a.aboutKey(key) {
		return a.openAbout()
	}
	if a.statusKey(key) {
		return a.writeStatus(key)
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

// submit runs Plans' field if it is a /word: /help or /breakdance
// (docs/ux.md §2).
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
		a.glance = 0 // it plays whole
	}
	return true
}

func (a *app) tapeKey(key string) bool {
	if len(a.log) == 0 && pageKey(&a.emptyScroll, key, a.layout().bodyH) {
		return true // the empty tab is a page (docs/ux.md §10)
	}
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
	}
	return false
}

func (a *app) planKey(key string) bool {
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
		b.Move(-len(b.Rows))
		return true
	case "end":
		b.Move(len(b.Rows))
		return true
	case "esc":
		// Esc cancels New plan, clears Find's or the filter's text, then
		// climbs one level; it never leaves PLAN (docs/ux.md §3).
		if a.draft != "" && !b.Prompting() {
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
		// enter on Plans' cards and rows, and in Open a plan; not in a find,
		// nor in New plan (docs/ux.md §3).
		if b.Prompting() || b.Screen == ScreenHome && a.draft != "" {
			return false
		}
		return a.planEnter()
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

// planBack goes up one level inside PLAN (docs/ux.md §3): New plan back to
// Plans, nothing written; a folder up to its parent, and rex/plans itself
// to Plans. Plans is the top.
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
	}
	return false
}

// planEnter is enter on Plans (docs/ux.md §11): New plan creates; Continue
// and Open latest open their plan following, and any other plan, match or
// creation latches it.
func (a *app) planEnter() bool {
	b := a.plans
	if b.Prompting() {
		held := a.pending
		if b.CommitCreate(a.draft) == PlanOpenChat {
			a.draft = ""
			a.openedPlan()
			a.created()
			a.latch() // you made it: latched (docs/ux.md §11)
			a.offerHeld(held)
		}
		return true // a refused name keeps its text, to fix
	}
	held := a.pending
	var kind PlanRowKind = -1
	if b.Cursor < len(b.Rows) {
		kind = b.Rows[b.Cursor].Kind
	}
	switch b.Activate() {
	case PlanStartCreate:
		a.draft = ""
	case PlanOpenChat:
		a.openedPlan()
		switch kind {
		case PlanRowContinue, PlanRowLatest:
			a.follow = true // the plan you had, or the newest: following (docs/ux.md §11)
		case PlanRowCreateNamed:
			a.created()
			a.latch()
		default:
			a.latch()
		}
		a.offerHeld(held)
	}
	return true
}

// openedPlan runs after a plan is opened, created or attached: Rex reads
// it, lands PLAN on its task list with the pin selected, says which file
// opened on the notice row, and remembers it as this repo's last plan.
func (a *app) openedPlan() {
	path := a.plans.Current
	now := a.now()
	if a.doc == nil || !samePlan(a.doc.path, path) {
		a.doc = openDoc(path, now)
		if _, err := os.Stat(path); err == nil && a.planFile(path, "") == "" {
			a.doc.outside = true
		}
		a.outline = outlineView{sel: defaultSel(a.doc.plan)}
		a.toast = StatusLine{} // the last plan's news isn't this one's
		a.unsaid, a.mine, a.late = nil, nil, nil
	}
	a.plans.showDoc(a.doc)
	// Reopening the plan already open (Continue) keeps its last good copy,
	// its debounce and its selection. Plans' field is done with (an attach
	// never comes while it holds any text, docs/ux.md §11).
	a.draft = ""
	a.at = atOutline
	a.offer, a.pending = "", "" // any open settles which plan is shown
	full, rel := "Opened ", a.plans.CurrentRel()
	if t := a.planTitle(); t != "" {
		full += t + " · "
	}
	a.show(openNotice(full+rel, "Opened ", rel))
	a.glanceOnce() // a reload never comes through here
	a.syncDrift()
	_ = plan.UpdateState(a.statePath, now, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: path, Opened: float64(now.Unix())}
	})
}

// created says the plan you just made was written (docs/ux.md §6),
// "✔ Created rex/plans/team/auth.rex.json", over openedPlan's notice.
func (a *app) created() {
	rel := a.plans.CurrentRel()
	a.show(openNotice(doneMark+" Created "+rel, doneMark+" Created ", rel))
}

// planTitle is the open plan's title as a notice names it, or "".
func (a *app) planTitle() string {
	if a.doc == nil || a.doc.plan == nil {
		return ""
	}
	return strings.TrimSpace(flat(a.doc.plan.Title))
}

// openNotice is the notice that says which file just opened (docs/ux.md
// §4, §6), "Opened Send-off demo · rex/plans/cuj/sendoff-demo.rex.json":
// narrow, it is lead and the path, then the path clips from the left.
func openNotice(full, lead, rel string) StatusLine {
	full, rel = flat(full), flat(rel)
	return StatusLine{Text: full, Kind: "text", Fit: func(w int) string {
		switch short := lead + rel; {
		case cellLen(short) <= w:
			return short
		case w-cellLen(lead) > headPathMin:
			return lead + "…" + tailCells(rel, w-cellLen(lead)-1)
		}
		return ellipsis(full, w)
	}}
}

// docKey handles keys on the open plan's task list and its pages. They
// have no text field: every key acts at once (docs/ux.md §3).
func (a *app) docKey(key string) bool {
	v := &a.outline
	p := a.doc.plan
	step := max(1, a.planRows()-2)
	if a.at == atOutline {
		if p == nil || len(p.Tasks) == 0 {
			// No list: the page scrolls, clamped when drawn (docs/ux.md §6).
			if key == "esc" || key == "left" {
				return a.toLibrary()
			}
			return pageKey(&v.scroll, key, a.planRows())
		}
		switch key {
		case "up", "down":
			v.move(p, map[string]int{"up": -1, "down": 1}[key])
			return true
		case "pageup", "pagedown":
			v.sel = outlinePage(p, *v, a.layout().textW, a.planRows(), a.lines >= blankFrom, key == "pagedown")
			return true
		case "home", "end":
			v.edge(p, key == "end")
			return true
		case "right", "\r", "\n":
			return a.openDetail()
		case "esc", "left":
			return a.toLibrary()
		}
		return false
	}
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
	case "esc", "left":
		// Back to the list, on the task it showed (docs/ux.md §3), or on the
		// pin when that task has left the file.
		a.at = atOutline
		a.outline.keepSel(a.doc.plan)
		return true
	}
	return false
}

// openDetail is enter or → on the task list: the selected task's details,
// on a page of their own (docs/ux.md §6).
func (a *app) openDetail() bool {
	if a.doc.plan == nil || a.outline.sel == "" {
		return false
	}
	a.at = atTaskDetail
	a.outline.scroll = 0
	return true
}

// aboutKey reports that key is i opening About from the task list
// (docs/ux.md §3): a letter that types nothing there.
func (a *app) aboutKey(key string) bool {
	return key == "i" && a.pane == PanePlans && a.at == atOutline && a.overlay[PanePlans] == overlayNone && a.doc != nil && a.doc.plan != nil
}

// openAbout is i on the task list: About, from its top.
func (a *app) openAbout() bool {
	a.at = atPlanDetail
	a.outline.scroll = 0
	return true
}

// toLibrary leaves the open plan for Plans, the cursor on Continue, the
// plan you left (docs/ux.md §11), so esc enter is a round trip. The plan
// stays open: AGENT keeps it.
func (a *app) toLibrary() bool {
	b := a.plans
	a.at = atLibrary
	a.draft = ""
	b.Screen, b.Find, b.Filter = ScreenHome, "", ""
	b.fresh = true
	b.Rebuild()
	return true
}

// pollDoc re-reads the open plan on the poll's clock. An edit keeps the
// selection, and once it is credited (readDoc, announce) shows a notice
// and leaves one "plan" line on AGENT. The agent pins and finishes tasks
// this way, and so does anyone editing the file: when the file marks the
// pin done, the notice says so and names what is next up (docs/ux.md §7b,
// §7, §8).
func (a *app) pollDoc() bool { return a.readDoc(false) }

// reloadDoc re-reads the open plan now, without waiting for the next poll:
// your agent just saved it, so the change is your agent's.
func (a *app) reloadDoc() bool {
	if a.doc == nil {
		return false
	}
	a.doc.polled = time.Time{}
	return a.readDoc(true)
}

// doneToast is the notice row's news when the file marks the old pin done: what is
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
	return StatusLine{Text: doneMark + " " + id + " done" + tail, Kind: "text", Fit: func(w int) string { return fitDone(id, tail, short, w) }}
}

// fitDone fits the done toast to width cells. What is next matters more
// than what just finished, so the done id clips from the left first
// ("✔ …ghlight done · next up load-outline"), down to headPathMin cells,
// then the tail shortens.
func fitDone(id, tail, short string, width int) string {
	for _, t := range []string{tail, short} {
		room := width - cellLen(doneMark+"  done"+t)
		if cellLen(id) <= room {
			return doneMark + " " + id + " done" + t
		}
		if room >= headPathMin {
			return doneMark + " …" + tailCells(id, room-1) + " done" + t
		}
	}
	return doneMark + " done" + short
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
	return append(help, a.health.lines(width, a.hidden, a.skill.look(a.plans.Repo, a.now()), time.Now())...)
}

// pageKind names the page Help opens over, for its THIS PAGE section.
func (a *app) pageKind() string {
	switch {
	case a.pane == PaneChat:
		return "agent"
	case a.at == atLibrary || a.doc == nil:
	case a.at == atPlanDetail:
		return "about"
	case a.at == atTaskDetail:
		return "task"
	case a.doc.plan == nil:
		return "can't read"
	case len(a.doc.plan.Tasks) == 0:
		return "no tasks"
	default:
		return "tasks"
	}
	switch a.plans.Screen {
	case ScreenBrowse:
		return "open a plan"
	case ScreenCreate:
		return "new plan"
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
// went from before lines. The change is signed (a block can take fewer
// rows than it did), and a scrolled view stays scrolled (docs/ux.md §10).
func (a *app) below(before int) {
	if a.scroll > 0 {
		a.scroll = max(1, a.scroll+a.logLines()-before)
	}
}

// syncPlanInput hands Plans' field to the screen that reads it: Open a
// plan filters, Plans finds, New plan previews where it will create. A
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
	// Spaces alone don't filter or find anything, and on Plans neither does
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
	case ScreenCreate:
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
	if a.creditDue() {
		changed = true
	}
	if a.scanPlans() {
		changed = true
	}
	if a.toast.Text != "" && !a.toastLive() {
		a.toast, a.toastDone = StatusLine{}, false
		changed = true
	}
	if a.quitPending {
		a.quitTicks--
		if a.quitTicks <= 0 {
			a.quitPending = false
			changed = true
		}
	}
	if a.glance > 0 {
		a.glance--
		if a.glance == 0 {
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
		if a.glance == 0 { // under the glance it shows once the glance ends
			changed = true
		}
	}
	return changed
}

// settle runs after every key or tick: it applies the field's text, opens
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
	return changed
}

const draftCap = 256

// capDraft keeps the field to draftCap whole runes.
func capDraft(s string) string {
	if r := []rune(s); len(r) > draftCap {
		return string(r[:draftCap])
	}
	return s
}
