package tui

import (
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/mascot"
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
}

// logViewCache keeps the laid-out tape for one log revision and width; the
// loop needs it several times per tick.
type logViewCache struct {
	rev, width int
	lines      []LogLine
	ok         bool
}

func newApp(color string, enabled bool, animation string, plans *PlanBrowser, columns, lines int) *app {
	a := &app{color: color, enabled: enabled, pane: PanePlans, plans: plans, columns: columns, lines: lines, scope: watch.NewScope(plans.Repo)}
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
			opt.LogView = a.health.lines(textW, a.hidden, time.Now())
		}
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

// placeholder is the dim hint in an empty composer: what you can type here.
func (a *app) placeholder() string {
	if a.overlay[a.pane] != overlayNone {
		return "esc to close"
	}
	if a.pane == PaneChat {
		return "note (not sent) · ?⏎ help"
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
	if a.pane == PanePlans && a.overlay[a.pane] == overlayNone && !forcedVerb(text) {
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
	return ""
}

// statusLine picks row 1. It is empty until the plan slices add the pin,
// drift and plan errors here (docs/ux.md §4).
func (a *app) statusLine() StatusLine {
	return StatusLine{}
}

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
	fresh = a.own(fromFeed(fresh))
	if len(fresh) == 0 {
		return false
	}
	before := a.logLines()
	prevTurn := a.turn
	for _, ev := range fresh {
		a.turn = ApplyTurn(a.turn, ev)
		if watch.KeepInPane(ev) || ev.Kind == "idle" { // a turn end clears an old error
			a.lastKind = ev.Kind
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

// openedPlan runs after a plan is opened or created. Until the outline
// exists (docs/ux.md §6) Rex moves to the tape, as it always has.
func (a *app) openedPlan() {
	// Keep the plan draft (a Browse filter) for Tab back, so the opened
	// plan is still the selected row.
	a.draft, a.parked = a.parked, a.draft
	a.pane = PaneChat
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
	return append(help, a.health.lines(width, a.hidden, time.Now())...)
}

// syncPlanInput hands the plan draft to the screen that reads it: Browse
// filters, Home finds, the name prompt previews where it will create. A
// new query puts the cursor on its first match.
func (a *app) syncPlanInput() bool {
	b := a.plans
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
	changed := false
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
