package tui

import (
	"os"
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

	pane   PaneKind
	plans  *PlanBrowser
	draft  string // the current place's draft
	parked string // the other place's draft, kept across Tab

	overlay       [2]overlayKind // per place
	overlayScroll [2]int

	log      []watch.Event
	logRev   int
	view     logViewCache
	scroll   int
	turn     Turn
	lastKind string

	quitPending bool
	quitTicks   int
	quit        bool
	repaint     bool // Ctrl+L asked for a full repaint
	feed        string

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
}

// logViewCache keeps the laid-out tape for one log revision and width; the
// loop needs it several times per tick.
type logViewCache struct {
	rev, width int
	lines      []LogLine
	ok         bool
}

func newApp(color string, enabled bool, animation string, plans *PlanBrowser, columns, lines int) *app {
	a := &app{color: color, enabled: enabled, pane: PanePlans, plans: plans, columns: columns, lines: lines, scope: watch.NewScope(plans.Repo),
		now: time.Now, statePath: plan.StatePath()}
	if plans.Current == "" {
		// The Continue card offers the plan last opened in this repo.
		if last := plan.LoadState(a.statePath).Repos[plans.Repo].Plan; last != "" && IsPlanFile(last) {
			if _, err := os.Stat(last); err == nil {
				plans.Continue = last
				plans.fresh = true
				plans.Rebuild()
			}
		}
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
		Placeholder:   a.placeholder(),
		Preview:       a.preview(),
		Enabled:       a.enabled,
		Columns:       a.columns,
		Lines:         a.lines,
		AnimationName: a.sequence.Name,
		Draft:         a.draft,
		Log:           a.log,
		QuitPending:   a.quitPending,
		Status:        statusForTurn(a.turn, a.lastKind),
		Scroll:        a.scroll,
		Pane:          a.pane,
		Plans:         a.plans,
		PlanRel:       a.plans.CurrentRel(),
		Line:          a.statusLine(),
	}
	textW := PaneTextWidth(a.columns)
	if textW > 0 {
		opt.LogView = a.logView(textW)
		if len(a.log) == 0 {
			a.health.refresh(time.Now())
			opt.LogView = a.health.lines(textW, a.hidden, time.Now(), true)
		}
	}
	if a.pane == PanePlans && a.at != atLibrary && a.doc != nil {
		l := layoutFor(a.columns, a.lines)
		opt.PlanView, opt.PlanScroll = a.planView(l.textW, l.bodyH)
		opt.PlanFooter = a.planFooter()
	}
	if a.overlay[a.pane] == overlayHelp {
		other := "tab tape"
		if a.pane == PaneChat {
			other = "tab plan"
		}
		body := a.helpBody(textW)
		footer := []string{"help", "esc close", other}
		if len(body) > PaneBodyHeight(a.lines) {
			footer = []string{"help", "esc close", other, "↑↓ scroll"}
		}
		opt.Overlay = &Overlay{Lines: body, Footer: footer, Scroll: a.overlayScroll[a.pane]}
	}
	return opt
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
		body = outlineBody(a.doc, v, width, viewH)
		head := headOf(body)
		pinned := planHead(body[:head], len(body), viewH)
		v.view = windowStart(body[head:], viewH-len(pinned), v.view)
		return body, v.view
	}
	v.scroll = max(0, min(v.scroll, len(body)-viewH))
	return body, v.scroll
}

// planFooter is the footer for the open plan's screens (docs/ux.md §4).
func (a *app) planFooter() []string {
	if a.at == atPlanDetail || a.at == atTaskDetail {
		return []string{"detail", "esc outline", "↑↓ scroll", "tab tape"}
	}
	if pv := a.preview(); pv != "" {
		return []string{"outline", pv}
	}
	if p := a.doc.plan; p != nil && p.ReadOnly != "" {
		return []string{"outline", "read-only", "esc plans", "tab tape"}
	}
	if p := a.doc.plan; p == nil || len(p.Tasks) == 0 {
		return []string{"outline", "tab tape", "esc plans"}
	}
	return []string{"outline", "tab tape", "esc plans", "⏎ detail", "?⏎ help"}
}

// verbHints are the verbs worth typing on the open plan, as the outline's
// placeholder: the ones this build offers.
func (a *app) verbHints() string {
	var hints []string
	for _, h := range []struct{ verb, hint string }{
		{"sendoff", "s⏎ sendoff"}, {"done", "d⏎ done"}, {"pin", "p⏎ pin"}, {"help", "?⏎ help"},
	} {
		if a.offers(h.verb) {
			hints = append(hints, h.hint)
		}
	}
	return strings.Join(hints, " · ")
}

// placeholder is the dim hint in an empty composer: what you can type here.
func (a *app) placeholder() string {
	if a.overlay[a.pane] != overlayNone {
		return "esc to close"
	}
	if a.pane == PaneChat {
		return "note (not sent) · ?⏎ help"
	}
	if a.at != atLibrary {
		return a.verbHints()
	}
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

// preview says what ⏎ will do with the current draft, for the footer.
func (a *app) preview() string {
	text := strings.TrimSpace(a.draft)
	if text == "" {
		return ""
	}
	if a.pane == PanePlans && a.at == atLibrary && a.overlay[a.pane] == overlayNone && !forcedVerb(text) {
		return ""
	}
	if verb, arg, ok := parseVerb(text); ok && a.offers(verb) {
		if arg != "" {
			return "⏎ " + verb + " " + arg
		}
		return "⏎ " + verb
	}
	if forcedVerb(text) {
		return "⏎ not a verb"
	}
	if a.pane == PaneChat {
		return "⏎ note (local, not sent)"
	}
	if a.pane == PanePlans && a.at != atLibrary && a.overlay[a.pane] == overlayNone {
		return "⏎ not a verb" // the outline and detail take verbs only
	}
	return ""
}

// statusLine picks row 1, the first of these that applies (docs/ux.md
// §4): the plan file can't be read, a toast, the pin.
func (a *app) statusLine() StatusLine {
	if s, ok := docStatus(a.doc); ok {
		return s
	}
	if a.toast.Text != "" && a.now().Before(a.toastUntil) {
		return a.toast
	}
	if a.doc == nil {
		return StatusLine{}
	}
	return pinStatus(a.doc.plan)
}

// flash shows a toast on row 1 for a few seconds.
func (a *app) flash(text, kind string) {
	a.toast = StatusLine{Text: text, Kind: kind}
	a.toastUntil = a.now().Add(toastFor)
}

const toastFor = 3 * time.Second

// exitLine is printed once the screen is restored, so the terminal keeps a
// trace of where you were.
func (a *app) exitLine() string {
	// Printed after the terminal is restored: a plan's file name must not
	// reach it raw.
	if rel := strings.TrimSpace(strings.ReplaceAll(watch.CleanText(a.plans.CurrentRel()), "\n", " ")); rel != "" {
		return "rex: " + rel
	}
	return "rex: no plan open"
}

func (a *app) logView(width int) []LogLine {
	if !a.view.ok || a.view.rev != a.logRev || a.view.width != width {
		a.view = logViewCache{rev: a.logRev, width: width, lines: BuildLogView(a.log, width), ok: true}
	}
	return a.view.lines
}

func (a *app) logLines() int {
	textW := PaneTextWidth(a.columns)
	if textW <= 0 {
		return 0
	}
	return len(a.logView(textW))
}

// ingest applies hook events: turn state for every event, the tape for the
// ones worth drawing. A scrolled-up view keeps showing the same lines while
// new ones arrive below (until MaxLog trims the oldest).
func (a *app) ingest(fresh []watch.Event) bool {
	for _, ev := range fresh {
		a.health.saw(ev)
	}
	hidden := a.hidden
	fresh = a.own(fromFeed(fresh))
	if len(fresh) == 0 {
		// A hidden event still changes the count the empty tape shows.
		return a.hidden != hidden && len(a.log) == 0
	}
	before := a.logLines()
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
	added := ExtendLog(&a.log, fresh)
	if len(added) > 0 || len(a.log) != n {
		a.logRev++ // a trim to MaxLog changes the tape too
	}
	if len(added) > 0 && a.scroll > 0 {
		a.scroll += max(0, a.logLines()-before)
	}
	if len(added) > 0 || a.turn != prevTurn {
		a.syncTurn()
		return true
	}
	return false
}

// keys applies one read's keys in order, settling after each one, so a
// filter letter and ⏎ read together act like two separate presses.
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

// feedKinds are the kinds an adapter writes (agents.md, "Kinds").
var feedKinds = map[string]bool{
	"thought": true, "text": true, "user": true, "command": true,
	"question": true, "result": true, "idle": true, "error": true,
}

// fromFeed drops feed lines that claim to be Rex's own blocks (a note,
// the "before rex started" rule): those are made in this process only.
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
func (a *app) own(events []watch.Event) []watch.Event {
	var out []watch.Event
	for _, ev := range events {
		if in, known := a.scope.Owns(ev); known && !in {
			a.hidden++
			continue
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
	recent = a.own(fromFeed(recent))
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
	// The same merging and dedupe as the live tape, so a growing reply is
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

func (a *app) applyKey(key string) bool {
	if l := layoutFor(a.columns, a.lines); !l.typing && key != CtrlC && key != "\x0c" {
		_, paste := IsPaste(key)
		edits := paste || isPrintableKey(key) || key == "\x7f" || key == "\x08" || key == "\x15" || key == "\x17"
		submits := (key == "\r" || key == "\n") && a.draft != ""
		if l.locked || edits || submits {
			// The UI isn't visible (locked), or the draft isn't: nothing
			// may act on it blind. A swallowed key still disarms quit.
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
		// The first Ctrl+C clears the draft or closes the overlay, and arms quit.
		// Closing an overlay drops what was typed in it too, so it can't
		// land on the screen underneath.
		a.overlay[a.pane] = overlayNone
		a.draft = ""
		a.quitPending = true
		a.quitTicks = QuitWindowTicks
		return true
	case "\t":
		a.quitPending = false
		a.draft, a.parked = a.parked, a.draft
		if a.pane == PanePlans {
			a.pane = PaneChat
		} else {
			a.pane = PanePlans
			a.plans.Rebuild()
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

// handleKeyArmed applies a key once quit is not armed.
func (a *app) handleKeyArmed(key string) bool {
	if text, ok := IsPaste(key); ok {
		// A paste is typed into the draft as one line; it never submits.
		next := capDraft(a.draft + text)
		changed := next != a.draft
		a.draft = next
		return changed
	}
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
	if a.overlay[a.pane] != overlayNone {
		return a.overlayKey(key)
	}
	if a.pane == PanePlans {
		return a.planKey(key)
	}
	return a.tapeKey(key)
}

// submit runs the draft as a verb if it is one this build offers.
func (a *app) submit(text string) bool {
	verb, arg, ok := parseVerb(text)
	if !ok || !a.offers(verb) {
		return false
	}
	a.draft = ""
	a.runVerb(verb, arg)
	return true
}

// offers reports which verbs exist yet. The plan verbs (sendoff, brief,
// done, pin) arrive with their features.
func (a *app) offers(verb string) bool {
	switch verb {
	case "help", "breakdance":
		return true
	}
	return false
}

func (a *app) runVerb(verb, _ string) {
	switch verb {
	case "help":
		a.overlay[a.pane] = overlayHelp
		a.overlayScroll[a.pane] = 0
	case "breakdance":
		a.play("breakdance")
	}
}

func (a *app) tapeKey(key string) bool {
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
	case "esc":
		if a.draft == "" {
			return false
		}
		a.draft = ""
		return true
	case "\r", "\n":
		text := strings.TrimSpace(a.draft)
		if text == "" {
			return false
		}
		if a.submit(text) {
			return true
		}
		// Anything else is a local note: drawn muted on the tape, kept in
		// memory only, never written to the feed or sent to the agent.
		a.log = append(a.log, watch.NewEvent("rex", "note", text, nil))
		a.logRev++
		a.draft = ""
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
	bodyH := PaneBodyHeight(a.lines)
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
		b.Cursor = 0
		return true
	case "end":
		if n := len(b.Rows); n > 0 {
			b.Cursor = n - 1
		}
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
				b.Err = "'" + strings.TrimSpace(a.draft) + "' is not a verb · ?⏎ for help"
			}
			return true
		}
		return a.planEnter()
	}
	return false
}

// planBack goes up one level inside PLAN: cancel a name prompt, leave a
// folder, leave Browse for Home. At Home it does nothing.
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

func (a *app) planEnter() bool {
	b := a.plans
	switch b.Screen {
	case ScreenCreate:
		if b.CommitCreate(a.draft) == PlanOpenChat {
			a.draft = ""
			a.openedPlan()
		}
		return true
	case ScreenFolder:
		_ = b.CommitFolder(a.draft)
		a.draft = ""
		return true
	}
	filter := b.Filter
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
	}
	return true
}

// openedPlan runs after a plan is opened or created: Rex reads it, lands
// on its outline with the pin selected, and remembers it for this repo.
func (a *app) openedPlan() {
	path := a.plans.Current
	now := a.now()
	if a.doc == nil || a.doc.path != path {
		a.doc = openDoc(path, now)
		a.outline = outlineView{sel: defaultSel(a.doc.plan)}
		a.toast = StatusLine{} // the last plan's news isn't this one's
	}
	// Continue on the plan already open keeps its last good copy, its
	// debounce and its selection.
	a.at = atOutline
	a.draft = ""
	_ = plan.UpdateState(a.statePath, now, func(s *plan.State) {
		s.Repos[a.plans.Repo] = plan.RepoState{Plan: path, Opened: float64(now.Unix())}
	})
}

// docKey handles keys on the open plan's outline and detail screens.
func (a *app) docKey(key string) bool {
	v := &a.outline
	p := a.doc.plan
	step := max(1, layoutFor(a.columns, a.lines).bodyH-2)
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
			if a.draft != "" {
				return false
			}
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
		if a.draft != "" {
			if key == "left" {
				return false
			}
			a.draft = ""
			return true
		}
		if a.at == atOutline {
			return a.toLibrary()
		}
		a.at = atOutline
		a.outline.keepSel(a.doc.plan) // the task read in detail may be gone
		return true
	case "\r", "\n":
		text := strings.TrimSpace(a.draft)
		if text == "" {
			if a.at == atOutline {
				return a.openDetail()
			}
			return false
		}
		if !a.submit(text) {
			a.flash("'"+text+"' is not a verb · ?⏎ help", "error")
		}
		return true
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
// plan stays open: the header and the tape keep it.
func (a *app) toLibrary() bool {
	b := a.plans
	a.at = atLibrary
	a.draft = ""
	b.Screen, b.Find, b.Filter = ScreenHome, "", ""
	b.fresh = true
	b.Rebuild()
	return true
}

// pollDoc re-reads the open plan. An outside edit keeps the selection,
// shows a toast, and leaves one "plan" line on the tape.
func (a *app) pollDoc() bool {
	if a.doc == nil {
		return false
	}
	changed, summary := a.doc.poll(a.now())
	if !changed {
		return false
	}
	if a.at != atTaskDetail {
		a.outline.keepSel(a.doc.plan)
	}
	if summary != "" {
		a.flash("plan updated · "+summary, "accent")
		before := a.logLines()
		a.log = append(a.log, watch.NewEvent("rex", "plan", summary, nil))
		a.logRev++
		if a.scroll > 0 {
			a.scroll += max(0, a.logLines()-before)
		}
	}
	return true
}

// overlayKey handles keys while an overlay covers the current place.
func (a *app) overlayKey(key string) bool {
	switch key {
	case "esc":
		if a.draft != "" {
			a.draft = ""
			return true
		}
		a.overlay[a.pane] = overlayNone
		return true
	case "up", "down", "pageup", "pagedown":
		page := PaneBodyHeight(a.lines)
		// A page keeps two rows of overlap: the ↑/↓ more markers cover them.
		step := max(1, page-2)
		delta := map[string]int{"up": -1, "down": 1, "pageup": -step, "pagedown": step}[key]
		limit := max(0, len(a.helpBody(PaneTextWidth(a.columns)))-page)
		next := max(0, min(limit, a.overlayScroll[a.pane]+delta))
		changed := next != a.overlayScroll[a.pane]
		a.overlayScroll[a.pane] = next
		return changed
	case "\r", "\n":
		// Only the overlay's own inputs work in it; Help has none, so a
		// typed word is dropped, never run underneath.
		if strings.TrimSpace(a.draft) != "" {
			a.draft = ""
			return true
		}
	}
	return false
}

// helpBody is the Help overlay's lines at width.
func (a *app) helpBody(width int) []LogLine {
	help := append(helpLines(width), LogLine{Kind: "gap"})
	a.health.refresh(time.Now())
	return append(help, a.health.lines(width, a.hidden, time.Now(), false)...)
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

// settle runs after every key or tick: it applies the browse filter, picks
// up a resize, and keeps the tape scroll in range.
func (a *app) settle(columns, lines int) bool {
	changed := false
	// An open overlay owns the composer: nothing typed in it filters the
	// list underneath.
	if a.pane == PanePlans && a.overlay[PanePlans] == overlayNone && a.syncPlanInput() {
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

// capDraft keeps the composer to draftCap whole runes.
func capDraft(s string) string {
	if r := []rune(s); len(r) > draftCap {
		return string(r[:draftCap])
	}
	return s
}
