package tui

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// driftState is the drift seen while one task was the pin (docs/ux.md
// §9a). It lives in memory and on the tape only: never in the feed, the
// plan, or anything sent to the agent.
type driftState struct {
	pin    string
	seen   map[string]bool
	items  []plan.Drift
	latest *plan.Drift // what row 1 shows
}

var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// checkDrift judges one live event from this repo against the pin:
// completed edits and user prompts only.
func (a *app) checkDrift(ev watch.Event) {
	if a.doc == nil || a.doc.plan == nil {
		return
	}
	p := a.doc.plan
	pin := p.Graph().Pin()
	if pin == nil {
		return
	}
	var d plan.Drift
	switch {
	case ev.Kind == "result" && watch.IsFileEditEvent(ev):
		if ok, has := ev.Meta["ok"].(bool); has && !ok {
			return // a write that was denied didn't happen
		}
		rel, in := a.scope.Rel(watch.EventPath(ev))
		if !in || rel == "" || strings.HasPrefix(rel, "../") {
			return
		}
		d = plan.CheckEdit(p, pin, rel, foldPaths)
	case ev.Kind == "user" && ev.Source != "rex":
		d = plan.CheckPrompt(p, pin, ev.Text)
	default:
		return
	}
	a.noteDrift(pin.ID, d)
}

func (a *app) noteDrift(pin string, d plan.Drift) {
	if a.drift.pin != pin {
		a.drift = driftState{pin: pin, seen: map[string]bool{}}
	}
	s := &a.drift
	if d.Level == plan.OnPin {
		if s.latest != nil && s.latest.Level == plan.Soft {
			s.latest = nil // back on the pin: the soft notice can go
		}
		return
	}
	s.latest = &d
	if s.seen[d.Subject] {
		return
	}
	s.seen[d.Subject] = true
	s.items = append(s.items, d)
	var rows []string
	for _, it := range s.items {
		rows = append(rows, fmt.Sprintf("%-4s  %s (%s)", it.Level, it.Subject, it.Reason))
	}
	text := strings.Join(rows, "\n")
	// One block per pin, updated where it first appeared.
	for i := len(a.log) - 1; i >= 0; i-- {
		if e := &a.log[i]; e.Kind == "drift" && e.Meta["pin"] == pin {
			e.Text = text
			a.logRev++
			return
		}
	}
	before := a.logLines()
	a.log = append(a.log, watch.NewEvent("rex", "drift", text, map[string]any{"pin": pin}))
	a.logRev++
	if a.scroll > 0 {
		a.scroll += max(0, a.logLines()-before)
	}
}

// driftStatus is row 1's drift line for the current pin, if any.
func (a *app) driftStatus() (StatusLine, bool) {
	if a.drift.latest == nil || a.doc == nil || a.doc.plan == nil {
		return StatusLine{}, false
	}
	pin := a.doc.plan.Graph().Pin()
	if pin == nil || pin.ID != a.drift.pin {
		return StatusLine{}, false
	}
	d := a.drift.latest
	kind := "accent"
	if d.Level == plan.Hard {
		kind = "error"
	}
	return StatusLine{Text: "drift · " + d.Level.String() + ": " + d.Subject + " · " + d.Reason, Short: "drift · " + d.Level.String() + ": " + d.Subject, Kind: kind}, true
}
