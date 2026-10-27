package tui

import (
	"fmt"
	"path"
	"runtime"
	"sort"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// driftState is the drift seen while one task was the pin (docs/ux.md
// §9a). It lives in memory and on AGENT only: never in the feed, the
// plan, or anything sent to the agent.
type driftState struct {
	key   string // pinKey of the plan and pin it is about
	pin   string // the pin's id, for the block's title
	gen   int    // this stint's block on AGENT; each new pin gets its own
	seen  map[string]bool
	items []driftNote // the block's rows, in the order they came
	// Row 1 shows the last unresolved hard notice, else the last soft one.
	hard, soft *driftNote
	// Prompts that name another task, held until the turn ends: asking the
	// agent to switch tasks is how the pin moves (docs/ux.md §9a). Edits of
	// a file listed for a task one of them names wait with them.
	held []driftNote
}

// driftNote is one judgement the notice row may show, with what it judged, so it
// can be judged again when the plan changes.
type driftNote struct {
	d      plan.Drift
	rel    string // an edit's path
	prompt string // or the prompt
}

var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// pinKey names one task of one plan: a pin stint's drift belongs to it.
func pinKey(planPath, task string) string { return planPath + "#" + task }

// checkDrift queues one live event from this repo to be judged against
// the pin once its batch is on AGENT (so the block lands after it).
func (a *app) checkDrift(ev watch.Event) { a.driftQueue = append(a.driftQueue, ev) }

// applyDrift judges the queued events: completed edits and user prompts
// only. It reports whether the notice row or AGENT changed.
func (a *app) applyDrift() bool {
	queue := a.driftQueue
	a.driftQueue = nil
	changed := false
	for _, ev := range queue {
		if turnEdge(ev) && a.flushHeld() {
			changed = true
		}
		notes := a.judge(ev)
		if a.writesOpen(ev) && a.reloadDoc() {
			// The edits before this save are judged against the plan they were
			// made under, the ones after against what it wrote (a new pin, say);
			// a patch that wrote both counts each file at its lesser level.
			changed = true
			notes = lesser(notes, a.judge(ev))
		}
		// One event's paths go in order of level, so the notice ends on its worst
		// (a patch's on-pin file can't hide its off-pin one).
		sort.SliceStable(notes, func(i, j int) bool { return notes[i].d.Level < notes[j].d.Level })
		for _, n := range notes {
			if n.d.Names != "" || a.asked(n.d.Listed) {
				a.syncDrift()
				a.drift.held = append(a.drift.held, n)
				continue
			}
			if a.noteDrift(n) {
				changed = true
			}
		}
	}
	return changed
}

// lesser is each file of one event at its lesser drift of two judgements,
// or none when either plan had no pin to judge it against.
func lesser(was, now []driftNote) []driftNote {
	if len(was) != len(now) {
		return nil
	}
	for i := range was {
		if now[i].d.Level < was[i].d.Level {
			was[i] = now[i]
		}
	}
	return was
}

// asked reports whether a held prompt names task id. An edit of a file
// listed for it waits with that prompt: the agent asked to start id may
// edit its files before it saves the plan that makes id the pin.
func (a *app) asked(id string) bool {
	if id == "" {
		return false
	}
	a.syncDrift()
	for _, n := range a.drift.held {
		if n.rel == "" && n.d.Names == id {
			return true
		}
	}
	return false
}

// turnEdge reports that ev ends a turn, or starts the next one (a missed
// turn end): either way, the prompts held from the turn before are judged.
func turnEdge(ev watch.Event) bool {
	if ev.Source == "rex" {
		return false
	}
	return ev.Kind == "user" || ApplyTurn(TurnRunning, ev) == TurnIdle
}

// flushHeld records the held prompts as drift: their turn ended with the
// pin where it was, so naming another task wasn't a switch.
func (a *app) flushHeld() bool {
	a.syncDrift()
	held := a.drift.held
	a.drift.held = nil
	changed := false
	for _, n := range held {
		if a.noteDrift(n) {
			changed = true
		}
	}
	return changed
}

// judge is the drift of one event: one note per file a completed edit
// touched (an apply_patch can touch several), or one for a prompt. While
// a latched plan offers another, nothing is judged: the agent wrote that
// plan, so its work belongs to it, not to the latched pin (docs/ux.md §9a).
func (a *app) judge(ev watch.Event) []driftNote {
	if a.doc == nil || a.doc.plan == nil || (!a.follow && a.offer != "") {
		return nil
	}
	p := a.doc.plan
	pin := p.Graph().Pin()
	if pin == nil {
		return nil
	}
	switch {
	case completedEdit(ev): // a write that was denied didn't happen
		var notes []driftNote
		for _, path := range watch.EventPaths(ev) {
			rel, in := a.scope.Rel(path)
			if !in || rel == "" || strings.HasPrefix(rel, "../") {
				continue
			}
			notes = append(notes, driftNote{d: plan.CheckEdit(p, pin, rel, foldPaths), rel: rel})
		}
		return notes
	case ev.Kind == "user" && ev.Source != "rex":
		return []driftNote{{d: plan.CheckPrompt(p, pin, ev.Text), prompt: ev.Text}}
	}
	return nil
}

// syncDrift starts over when the effective pin (of this plan) is not the
// one the state is about: the notice clears, and the next drift starts a new
// block. A held prompt that named the new pin asked for this move and
// goes, with the edits held for it; one that names another task still
// waits for its turn to end, and so do the edits of that task's files.
func (a *app) syncDrift() {
	key, id := "", ""
	var p *plan.Plan
	var pin *plan.Task
	if a.doc != nil && a.doc.plan != nil {
		p = a.doc.plan
		if pin = p.Graph().Pin(); pin != nil {
			key, id = pinKey(a.doc.path, pin.ID), pin.ID
		}
	}
	if key == a.drift.key {
		return
	}
	held := a.drift.held
	a.driftGen++
	a.drift = driftState{key: key, pin: id, gen: a.driftGen, seen: map[string]bool{}}
	if pin == nil {
		return
	}
	asked := map[string]bool{}
	for i := range held {
		if n := &held[i]; n.rel == "" {
			if n.d = plan.CheckPrompt(p, pin, n.prompt); n.d.Names != "" {
				asked[n.d.Names] = true
			}
		}
	}
	for _, n := range held {
		if n.rel != "" {
			n.d = plan.CheckEdit(p, pin, n.rel, foldPaths)
		}
		if n.d.Names != "" || asked[n.d.Listed] {
			a.drift.held = append(a.drift.held, n)
		}
	}
}

// rejudgeDrift checks the drift notices against the plan as it is now: one
// that is on the pin after an edit to the plan (it was widened) goes.
func (a *app) rejudgeDrift() {
	a.syncDrift()
	if a.doc == nil || a.doc.plan == nil {
		return
	}
	p := a.doc.plan
	pin := p.Graph().Pin()
	if pin == nil {
		return
	}
	again := func(n *driftNote) *driftNote {
		if n == nil {
			return nil
		}
		d := plan.CheckPrompt(p, pin, n.prompt)
		if n.rel != "" {
			d = plan.CheckEdit(p, pin, n.rel, foldPaths)
		}
		if d.Level == plan.OnPin {
			return nil
		}
		n.d = d
		return n
	}
	a.drift.hard, a.drift.soft = again(a.drift.hard), again(a.drift.soft)
	if a.drift.hard != nil && a.drift.hard.d.Level != plan.Hard {
		a.drift.soft, a.drift.hard = a.drift.hard, nil
	}
}

func (a *app) noteDrift(n driftNote) bool {
	a.syncDrift()
	s := &a.drift
	d := n.d
	if d.Level == plan.OnPin {
		if n.rel == "" {
			return false // a prompt on the pin ("ok, continue") clears nothing
		}
		changed := s.soft != nil
		s.soft = nil // an edit back on the pin: the soft notice can go
		if s.hard != nil && s.hard.rel == n.rel {
			s.hard, changed = nil, true // the same file, now fine
		}
		return changed
	}
	switch {
	case d.Level != plan.Hard:
		s.soft = &n
	case a.pane != PaneChat:
		s.hard = &n // AGENT lists it, so seen there it is no notice (docs/ux.md §4)
	}
	if s.seen[d.Subject] {
		return true
	}
	s.seen[d.Subject] = true
	s.items = append(s.items, n)
	var text []string
	for _, it := range s.items {
		text = append(text, fmt.Sprintf("%s %s · %s", it.d.Level, it.d.Subject, it.d.Reason))
	}
	// AGENT draws the rows from meta, not by reading the text back: the
	// block is Rex's own and never reaches the feed.
	rows := append([]driftNote(nil), s.items...)
	// One block per pin stint, updated where it first appeared. A scrolled-
	// up view keeps showing the same lines: a block that ends inside it
	// grows into it, and one that ends under it moves the view by the rows
	// it gains.
	for i := len(a.log) - 1; i >= 0; i-- {
		if e := &a.log[i]; e.Kind == "drift" && e.Meta["gen"] == s.gen {
			before := a.logLines()
			// The rows under the block, measured from the head: blocks pack
			// or space by kind, so the tail alone can't say.
			below := a.scroll > 0 && before-len(BuildLogView(a.log[:i+1], PaneTextWidth(a.columns, a.lines), a.viewOpt())) <= a.scroll
			e.Text = strings.Join(text, "\n")
			e.Meta["rows"] = rows
			a.logRev++
			if below {
				a.below(before)
			}
			return true
		}
	}
	before := a.logLines()
	a.log = append(a.log, watch.NewEvent("rex", "drift", strings.Join(text, "\n"), map[string]any{"gen": s.gen, "pin": s.pin, "rows": rows}))
	a.logRev++
	a.below(before) // a new block below a scrolled-up view keeps it put
	return true
}

// driftStatus is the notice row's drift line for the current pin, if any
// (docs/ux.md §4, §9a): the last unresolved hard drift, "off pin" in the
// pin's yellow. Soft drift is AGENT's only.
func (a *app) driftStatus() (StatusLine, bool) {
	if a.doc == nil || a.doc.plan == nil {
		return StatusLine{}, false
	}
	pin := a.doc.plan.Graph().Pin()
	if pin == nil || pinKey(a.doc.path, pin.ID) != a.drift.key {
		return StatusLine{}, false
	}
	n := a.drift.hard
	if n == nil || n.d.Level != plan.Hard {
		return StatusLine{}, false
	}
	d := n.d
	// Short: a prompt's reason (its words are on AGENT), or an edit's file
	// name (the end of the path matters most).
	short := path.Base(d.Subject)
	if n.prompt != "" {
		short = d.Reason
	}
	return StatusLine{Text: offPin + " · " + d.Subject + " · " + d.Reason, Short: offPin + " · " + short, Kind: "text", Spans: func(text string) []Span {
		rest, _ := strings.CutPrefix(text, offPin)
		return []Span{{Text: offPin, Role: "accent"}, {Text: rest}}
	}}, true
}

// offPin leads the hard drift notice, in the pin's yellow: off the pin,
// never an error's red.
const offPin = "off pin"
