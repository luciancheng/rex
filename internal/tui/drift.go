package tui

import (
	"fmt"
	"path"
	"runtime"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// driftState is the drift seen while one task was the pin (docs/ux.md
// §9a). It lives in memory and on the tape only: never in the feed, the
// plan, or anything sent to the agent.
type driftState struct {
	key   string // plan.PinKey of the plan and pin it is about
	pin   string // the pin's id, for the block's title
	gen   int    // this stint's block on the tape; each new pin gets its own
	seen  map[string]bool
	items []plan.Drift
	// Row 1 shows the last unresolved hard notice, else the last soft one.
	hard, soft *driftNote
}

// driftNote is one judgement row 1 may show, with what it judged, so it
// can be judged again when the plan changes.
type driftNote struct {
	d      plan.Drift
	rel    string // an edit's path
	prompt string // or the prompt
}

var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// checkDrift queues one live event from this repo to be judged against
// the pin once its batch is on the tape (so the block lands after it).
func (a *app) checkDrift(ev watch.Event) { a.driftQueue = append(a.driftQueue, ev) }

// applyDrift judges the queued events: completed edits and user prompts
// only. It reports whether row 1 or the tape changed.
func (a *app) applyDrift() bool {
	queue := a.driftQueue
	a.driftQueue = nil
	changed := false
	for _, ev := range queue {
		if n, ok := a.judge(ev); ok && a.noteDrift(n) {
			changed = true
		}
	}
	return changed
}

func (a *app) judge(ev watch.Event) (driftNote, bool) {
	if a.doc == nil || a.doc.plan == nil {
		return driftNote{}, false
	}
	p := a.doc.plan
	pin := p.Graph().Pin()
	if pin == nil {
		return driftNote{}, false
	}
	switch {
	case ev.Kind == "result" && watch.IsFileEditEvent(ev):
		if ok, has := ev.Meta["ok"].(bool); has && !ok {
			return driftNote{}, false // a write that was denied didn't happen
		}
		rel, in := a.scope.Rel(watch.EventPath(ev))
		if !in || rel == "" || strings.HasPrefix(rel, "../") {
			return driftNote{}, false
		}
		return driftNote{d: plan.CheckEdit(p, pin, rel, foldPaths), rel: rel}, true
	case ev.Kind == "user" && ev.Source != "rex":
		return driftNote{d: plan.CheckPrompt(p, pin, ev.Text), prompt: ev.Text}, true
	}
	return driftNote{}, false
}

// syncDrift starts over when the effective pin (of this plan) is not the
// one the state is about: row 1 clears, and the next drift starts a new
// block.
func (a *app) syncDrift() {
	key, id := "", ""
	if a.doc != nil && a.doc.plan != nil {
		if pin := a.doc.plan.Graph().Pin(); pin != nil {
			key, id = plan.PinKey(a.doc.path, pin.ID), pin.ID
		}
	}
	if key != a.drift.key {
		a.driftGen++
		a.drift = driftState{key: key, pin: id, gen: a.driftGen, seen: map[string]bool{}}
	}
}

// rejudgeDrift checks row 1's notices against the plan as it is now: one
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
	if d.Level == plan.Hard {
		s.hard = &n
	} else {
		s.soft = &n
	}
	if s.seen[d.Subject] {
		return true
	}
	s.seen[d.Subject] = true
	s.items = append(s.items, d)
	var rows []string
	for _, it := range s.items {
		rows = append(rows, fmt.Sprintf("%s %s (%s)", it.Level, it.Subject, it.Reason))
	}
	text := strings.Join(rows, "\n")
	// One block per pin stint, updated where it first appeared. A block
	// above a scrolled-up view grows out of sight; one below it shifts the
	// view by the rows it gains.
	for i := len(a.log) - 1; i >= 0; i-- {
		if e := &a.log[i]; e.Kind == "drift" && e.Meta["gen"] == s.gen {
			e.Text = text
			a.logRev++
			return true
		}
	}
	before := a.logLines()
	a.log = append(a.log, watch.NewEvent("rex", "drift", text, map[string]any{"gen": s.gen, "pin": s.pin}))
	a.logRev++
	if a.scroll > 0 {
		a.scroll += max(0, a.logLines()-before) // a new block below a scrolled-up view keeps it put
	}
	return true
}

// driftStatus is row 1's drift line for the current pin, if any.
func (a *app) driftStatus() (StatusLine, bool) {
	if a.doc == nil || a.doc.plan == nil {
		return StatusLine{}, false
	}
	pin := a.doc.plan.Graph().Pin()
	if pin == nil || plan.PinKey(a.doc.path, pin.ID) != a.drift.key {
		return StatusLine{}, false
	}
	n := a.drift.hard
	if n == nil {
		n = a.drift.soft
	}
	if n == nil {
		return StatusLine{}, false
	}
	d := n.d
	kind := "accent"
	if d.Level == plan.Hard {
		kind = "error"
	}
	// Short: a prompt's reason (its words are on the tape below), or an
	// edit's file name (the end of the path matters most).
	short := path.Base(d.Subject)
	if n.prompt != "" {
		short = d.Reason
	}
	return StatusLine{Text: "drift · " + d.Level.String() + ": " + d.Subject + " · " + d.Reason, Short: "drift · " + d.Level.String() + ": " + short, Kind: kind}, true
}
