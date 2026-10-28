package tui

import (
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
	key string // pinKey of the plan and pin it is about
	// The notice row shows the last unresolved hard drift; the last soft
	// one is kept to clear it on an edit back on the pin.
	hard, soft *driftNote
	// Prompts that name another task, held until the turn ends: asking the
	// agent to switch tasks is how the pin moves (docs/ux.md §9a). Edits of
	// a file listed for a task one of them names wait with them.
	held []driftNote
}

// driftNote is one judgement the notice row may show, with what it judged, so it
// can be judged again when the plan changes, and where AGENT tags it.
type driftNote struct {
	d      plan.Drift
	rel    string // an edit's path
	prompt string // or the prompt
	row    rowID  // the judged event's place in the stream: a prompt's own row
	step   rowID  // an edit's step on AGENT; none, a step Rex draws
}

// driftKey marks an event of AGENT's stream with the drift it was judged
// (docs/ux.md §9a, §10): a driftTag, a type of Rex's own, so no feed line
// can carry one, and neither the tag nor its reason is ever read back
// from text.
const driftKey = "rex.drift"

// driftTag is a row's drift: its level, and why, as the judge said it.
type driftTag struct {
	level plan.DriftLevel
	why   string
}

// tagOf is the drift ev's row on AGENT is tagged with; none on the pin.
func tagOf(ev watch.Event) driftTag {
	t, _ := ev.Meta[driftKey].(driftTag)
	return t
}

// driftOf is the level of ev's tag: none, soft or hard.
func driftOf(ev watch.Event) plan.DriftLevel {
	return tagOf(ev).level
}

// rowKey marks each live event with a rowID of Rex's own, in the order the
// feed brought them: a prompt's drift, judged later (a held prompt, at its
// turn's end), tags that prompt's row and no other, and a completed edit
// finds its step among the rows that came before it, never after.
const rowKey = "rex.row"

type rowID int

func rowOf(ev watch.Event) rowID {
	id, _ := ev.Meta[rowKey].(rowID)
	return id
}

// claimKey marks a step a completed edit was matched to, so the next
// completion of that file takes the step after it, not the same one.
const claimKey = "rex.claimed"

type claimMark struct{}

// marked is a live event with a rowID of its own; its meta is copied, so
// whatever else holds the event's meta is left alone.
func (a *app) marked(ev watch.Event) watch.Event {
	a.lastRow++
	ev.Meta = withMeta(ev.Meta, rowKey, a.lastRow)
	return ev
}

// withMeta is a copy of meta with k set to v.
func withMeta(meta map[string]any, k string, v any) map[string]any {
	out := make(map[string]any, len(meta)+1)
	for key, val := range meta {
		out[key] = val
	}
	out[k] = v
	return out
}

var foldPaths = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// pinKey names one task of one plan: a pin stint's drift belongs to it.
func pinKey(planPath, task string) string { return planPath + "#" + task }

// checkDrift queues one live event from this repo to be judged against
// the pin once its batch is on AGENT (so its row is there to tag).
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
		if len(notes) > 0 && notes[0].rel != "" {
			// Its step now, in feed order: a held edit's turn may be over by
			// the time it is tagged.
			step := a.claimStep(ev, notes)
			for i := range notes {
				notes[i].step = step
			}
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
			notes = append(notes, driftNote{d: plan.CheckEdit(p, pin, rel, foldPaths), rel: rel, row: rowOf(ev)})
		}
		return notes
	case ev.Kind == "user" && ev.Source != "rex":
		return []driftNote{{d: plan.CheckPrompt(p, pin, ev.Text), prompt: ev.Text, row: rowOf(ev)}}
	}
	return nil
}

// syncDrift starts over when the effective pin (of this plan) is not the
// one the state is about: the notice clears, and the tags already drawn
// stay. A held prompt that named the new pin asked for this move and goes,
// with the edits held for it; one that names another task still waits for
// its turn to end, and so do the edits of that task's files.
func (a *app) syncDrift() {
	key := ""
	var p *plan.Plan
	var pin *plan.Task
	if a.doc != nil && a.doc.plan != nil {
		p = a.doc.plan
		if pin = p.Graph().Pin(); pin != nil {
			key = pinKey(a.doc.path, pin.ID)
		}
	}
	if key == a.drift.key {
		return
	}
	held := a.drift.held
	a.drift = driftState{key: key}
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

// noteDrift records one judgement: hard drift on the notice row (but
// while AGENT, which shows it, is on screen), and every drift as a tag on
// its row on AGENT (docs/ux.md §9a, §10). It reports whether either
// changed.
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
		s.hard = &n // AGENT tags it, so seen there it is no notice (docs/ux.md §4)
	}
	a.tagDrift(n)
	return true
}

// tagDrift puts a judgement's tag on its row on AGENT (docs/ux.md §10): a
// prompt's own row, or an edit's step (claimStep). A tag only ever rises
// (soft, then hard), taking the rising note's reason, and it never adds or
// takes away a row. An edit with no step on AGENT gets a step row of Rex's
// own, drawn from the drift's meta; a scrolled-up view keeps its lines.
func (a *app) tagDrift(n driftNote) {
	want := n.row
	if n.rel != "" {
		want = n.step
	}
	for i := len(a.log) - 1; want != 0 && i >= 0; i-- {
		if e := &a.log[i]; rowOf(*e) == want {
			if n.d.Level > driftOf(*e) {
				why := n.d.Reason
				if n.rel != "" && a.shownRel(*e) != n.rel {
					// A patch's step names its first file: another's reason
					// says whose it is.
					why = path.Base(n.rel) + ": " + why
				}
				e.Meta = withMeta(e.Meta, driftKey, driftTag{n.d.Level, why})
				a.logRev++
			}
			return
		}
	}
	if n.rel == "" {
		return
	}
	// The row carries its completion's place, so a later completion of the
	// same file in this turn tags it rather than drawing another.
	before := a.logLines()
	a.log = append(a.log, watch.NewEvent("rex", "drift", "", map[string]any{driftKey: driftTag{n.d.Level, n.d.Reason}, "rel": n.rel, rowKey: n.row}))
	a.logRev++
	a.below(before) // a new row below a scrolled-up view keeps it put
}

// claimStep is the step on AGENT a completed edit, ev, belongs to, and
// marks it taken (docs/ux.md §9a): searching this turn's rows from before
// ev (the whole batch is on AGENT by now), the step with its tool call,
// else, when either names none, the newest edit step of one of its files
// that no earlier completion took. A repeat (Cursor's afterFileEdit after
// its postToolUse) finds none untaken and takes the step, or the step Rex
// drew, of the completion it repeats. None: Rex draws one (tagDrift).
func (a *app) claimStep(ev watch.Event, notes []driftNote) rowID {
	at, call := rowOf(ev), toolCall(ev)
	var again rowID
	for i := len(a.log) - 1; i >= 0; i-- {
		e := &a.log[i]
		r := rowOf(*e)
		switch {
		case r == 0 || r >= at:
			continue
		case e.Kind == "user" && e.Source != "rex":
			return again // the turn's start
		case !a.stepEdits(*e, call, notes):
			continue
		}
		if _, taken := e.Meta[claimKey].(claimMark); taken || e.Source == "rex" {
			again = max(again, r)
			continue
		}
		e.Meta = withMeta(e.Meta, claimKey, claimMark{})
		return r
	}
	return again
}

// shownRel is the file a step's row names: the step's first path (a
// patch's row says "+N more" for the rest), or the file of a step Rex drew.
func (a *app) shownRel(e watch.Event) string {
	if rel, ok := e.Meta["rel"].(string); ok && e.Source == "rex" {
		return rel
	}
	if paths := watch.EventPaths(e); len(paths) > 0 {
		rel, _ := a.scope.Rel(paths[0])
		return rel
	}
	return ""
}

// toolCall is the tool call an event names, if any.
func toolCall(ev watch.Event) string {
	call, _ := ev.Meta["tool_use_id"].(string)
	return call
}

// stepEdits reports that e, a step on AGENT (or one Rex drew), is an edit
// of the completion with call and notes: the same tool call, or, when
// either names none, an edit step of one of its files.
func (a *app) stepEdits(e watch.Event, call string, notes []driftNote) bool {
	files := map[string]bool{}
	for _, n := range notes {
		files[n.rel] = true
	}
	if e.Source == "rex" {
		rel, _ := e.Meta["rel"].(string)
		return e.Kind == "drift" && files[rel]
	}
	if e.Kind != "command" {
		return false
	}
	if c := toolCall(e); c != "" && call != "" {
		return c == call
	}
	if !watch.IsFileEditTool(watch.MetaTool(e)) {
		return false
	}
	for _, p := range watch.EventPaths(e) {
		if rel, in := a.scope.Rel(p); in && files[rel] {
			return true
		}
	}
	return false
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
