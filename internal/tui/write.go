package tui

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
)

// You and your agent both drive (docs/ux.md §7, §7b, §8): p pins the
// selected task or unpins the pin, d marks it done or reopens it. Rex
// writes only status values, the same ones the rex-plan skill has the
// agent write, through plan.Edit; every reload of the plan says who
// changed it.

// statusKey reports that key is p or d acting on a task now: on the task
// list or a task's details, with a task there to act on. Letters that type
// nothing there, so they work on a short screen too.
func (a *app) statusKey(key string) bool {
	if key != "p" && key != "d" || a.pane != PanePlans || a.overlay[PanePlans] != overlayNone {
		return false
	}
	if a.at != atOutline && a.at != atTaskDetail || a.doc == nil || a.doc.plan == nil {
		return false
	}
	return findTask(a.doc.plan, a.outline.sel) != nil
}

// writeStatus is p or d on the selected task (docs/ux.md §7b): what
// you saw (the task, its status, and for a pin the other active tasks)
// goes with the key, and the write lands only if the file still says so.
// A refusal is a notice and writes nothing.
func (a *app) writeStatus(key string) bool {
	d := a.doc
	t := findTask(d.plan, a.outline.sel)
	if why := d.unwritable(); why != "" {
		a.flash(why, "error")
		return true
	}
	if t.ID == "" {
		a.flash("This task has no id · give it one in the file first", "error")
		return true
	}
	// Only a plan file under rex/plans is written, its links followed
	// (plan.Jail): a link out of it leads nowhere, and its pin is never
	// sent either.
	path := a.planFile(d.path, "")
	if path == "" {
		a.flash(outsideNotice, "error")
		return true
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	s := plan.Seen{ID: t.ID, Status: t.Status}
	var op plan.Op
	switch pin := d.plan.Graph().Pin(); {
	case key == "p" && t == pin && t.Status == plan.Active:
		// Only the pin unpins: p on a second active task pins it, which
		// moves the pin off the first.
		op = plan.UnpinOp(s)
	case key == "p":
		s.Moving = d.plan.Graph().Moving(t)
		op = plan.PinOp(s)
	case t.Status == plan.Done:
		op = plan.ReopenOp(s)
	default:
		op = plan.DoneOp(s)
	}
	frame := d.good
	saved, err := plan.Edit(path, op)
	if err != nil {
		a.refused(t.ID, err)
		return true
	}
	// What the file did since the frame rides along with this write: AGENT
	// records it, and the notice row is yours.
	a.flushUnsaid()
	if saved.From != frame && d.plan != nil {
		if news := planChanges(d.plan, saved.Base); news.text != "" {
			a.tapeLine("plan", news.text)
		}
	}
	d.took(saved)
	a.trackPin()
	a.mine = mineOf(d.path, t.ID, key, saved)
	a.late = nil
	a.plans.showDoc(d)
	if a.at == atOutline {
		a.outline.keepSel(d.plan)
	}
	a.rejudgeDrift()
	notice, line := a.mine.said(saved.Base, saved.Plan, a.sendTail())
	a.show(notice)
	a.toastDone = strings.HasPrefix(notice.Text, doneMark+" ")
	a.tapeLine("plan", line)
	return true
}

// unwritable says why p and d can't write the open plan now, as the notice
// row says it, or "": a plan Rex can't read (it shows the last good copy,
// or nothing), a read-only one, or one whose file leads out of rex/plans
// (docs/ux.md §7b).
func (d *planDoc) unwritable() string {
	switch {
	case d.plan == nil || d.err != "" || d.gone:
		return "Can't read the plan · p and d are off"
	case d.plan.ReadOnly != "":
		return "Read-only: " + d.plan.ReadOnly + " · p and d are off"
	case d.outside:
		return outsideNotice
	}
	return ""
}

// outsideNotice refuses a plan whose file leads out of rex/plans.
const outsideNotice = "This plan isn't a file under rex/plans/ · p and d are off"

// refused says why a write didn't land (docs/ux.md §7b), on the
// notice row for 4 s. A task the file moved on since the frame is shown as
// the file has it now.
func (a *app) refused(id string, err error) {
	var stale *plan.StaleError
	text := ""
	var twice *plan.TwiceError
	switch {
	case errors.As(err, &stale):
		text = stale.ID + " just changed in the file · look again"
	case errors.As(err, &twice):
		text = "two tasks are called " + twice.ID + " · fix the plan first"
	case errors.Is(err, plan.ErrTwice):
		text = "two tasks are called " + id + " · fix the plan first"
	case errors.Is(err, plan.ErrBusy):
		text = "The plan keeps changing · try again"
	case errors.Is(err, plan.ErrReadOnly):
		why := strings.TrimPrefix(strings.TrimPrefix(err.Error(), plan.ErrReadOnly.Error()), ": ")
		text = "Read-only: " + why + " · p and d are off"
	case errors.Is(err, plan.ErrUnreadable):
		text = "Can't read the plan · nothing written"
	case errors.Is(err, plan.ErrGroup):
		text = id + " has tasks under it · pick one of those"
	case errors.Is(err, plan.ErrLoop):
		text = id + " is in a loop · fix the plan first"
	default:
		text = "Can't write the plan: " + cause(err)
	}
	a.flash(flat(text), "error")
	if stale != nil {
		a.doc.polled = time.Time{}
		a.pollDoc()
	}
}

// ownEdit is Rex's last write to the open plan: the task, the key, and the
// status it had and was given; and every member the write changed (the
// task, the tasks a pin moved off, the plan's own status) as it was, with
// the tasks whole. It lasts until the next reload that changes the plan,
// which says whether your agent saved over it (docs/ux.md §7b).
type ownEdit struct {
	path, id, key string
	was, wrote    plan.Status
	before        []plan.Change
	tasks         map[string]plan.Task
}

// mineOf is the write Rex just saved, on task id by key.
func mineOf(path, id, key string, saved *plan.Saved) *ownEdit {
	m := &ownEdit{path: path, id: id, key: key, tasks: map[string]plan.Task{}}
	if t := saved.Base.Task(id); t != nil {
		m.was = t.Status
	}
	if t := saved.Plan.Task(id); t != nil {
		m.wrote = t.Status
	}
	for _, c := range saved.Changes {
		if c.Task == "" {
			m.before = append(m.before, plan.SetStatus("", saved.Base.Status))
			continue
		}
		if t := saved.Base.Task(c.Task); t != nil {
			m.before = append(m.before, plan.SetStatus(c.Task, t.Status))
			m.tasks[c.Task] = *t
		}
	}
	return m
}

// said is the notice and AGENT's line for the write (docs/ux.md §7b),
// from the plan before and after it; sends is what a pin will do now, in
// the send state's words (sendTail).
func (m *ownEdit) said(before, after *plan.Plan, sends string) (StatusLine, string) {
	id := flat(m.id)
	g := after.Graph()
	switch {
	case m.key == "p" && m.wrote == plan.Active:
		var parts []notePart
		if moved := before.Graph().Moving(before.Task(m.id)); len(moved) > 0 {
			parts = append(parts, notePart{" · " + flat(strings.Join(moved, ", ")) + " unpinned", 0})
		}
		var waits []string
		for _, dep := range g.Unmet(g.Task(m.id)) {
			waits = append(waits, strings.TrimPrefix(dep, "?"))
		}
		if len(waits) > 0 {
			parts = append(parts, notePart{" · waits on " + flat(strings.Join(waits, ", ")), 3})
		}
		switch m.was {
		case plan.Done:
			parts = append(parts, notePart{" · reopened", 2})
		case plan.Blocked:
			parts = append(parts, notePart{" · it was blocked", 2})
		}
		return partsLine("Pinned "+id, sends, parts), "you pinned " + id
	case m.key == "p":
		tail := " · nothing is sent now"
		if pin := g.Pin(); pin != nil && pin.Status == plan.Active {
			tail = " · " + flat(pin.ID) + " is pinned now"
		}
		return partsLine("Unpinned "+id, tail, nil), "you unpinned " + id
	case m.wrote == plan.Done:
		tail, short := " · nothing pinned · nothing else is unblocked", " · nothing unblocked"
		switch pin := g.Pin(); {
		case pin != nil && pin.Status == plan.Active:
			tail, short = " · "+flat(pin.ID)+" still pinned", " · "+flat(pin.ID)+" pinned"
		case pin != nil:
			tail, short = " · nothing pinned · next up "+flat(pin.ID), " · next up "+flat(pin.ID)
		default:
			if done, total := g.Progress(); done == total {
				tail, short = " · the plan is done", " · plan done"
			}
		}
		return StatusLine{Text: doneMark + " " + id + " done" + tail, Kind: "text", Fit: func(w int) string { return fitDone(id, tail, short, w) }}, "you marked " + id + " done"
	}
	return partsLine(id+" reopened", "", nil), "you reopened " + id
}

// overwritten reports that your agent's save put back everything Rex's
// write changed: every member is back to what it was, and the tasks it
// touched are otherwise as they were. That is a save from a copy older
// than Rex's write. Anything else (your agent pinned another task, or
// finished the one you reopened, with what it did) is your agent's own
// move, credited as such. A save that only flips one status back and
// touches nothing else can't be told from an old copy, and counts as one.
func (m *ownEdit) overwritten(path string, p *plan.Plan) bool {
	if m == nil || p == nil || len(m.before) == 0 || !samePlan(m.path, path) {
		return false
	}
	g := p.Graph()
	for _, c := range m.before {
		if c.Task == "" {
			if p.Status != c.Status {
				return false
			}
			continue
		}
		t := g.Task(c.Task)
		if t == nil || t.Status != c.Status || !sameTask(*t, m.tasks[c.Task]) {
			return false
		}
	}
	return true
}

// sameTask reports two reads of a task that differ at most in their
// status and position.
func sameTask(a, b plan.Task) bool {
	a.Status, b.Status, a.Index, b.Index = "", "", 0, 0
	return reflect.DeepEqual(a, b)
}

// savedOver is the notice when your agent saved over Rex's write (docs/ux.md
// §7b): said once, never fought; the same key writes it again.
func (m *ownEdit) savedOver() StatusLine {
	id := flat(m.id)
	lead, again := "Your agent saved over your pin of "+id, " · p pins it again"
	switch {
	case m.key == "p" && m.wrote != plan.Active:
		lead, again = "Your agent saved over your unpin of "+id, " · p unpins it again"
	case m.key == "d" && m.wrote == plan.Done:
		lead, again = "Your agent saved over marking "+id+" done", " · d marks it again"
	case m.key == "d":
		lead, again = "Your agent saved over reopening "+id, " · d reopens it again"
	}
	return partsLine(lead, again, nil)
}

// notePart is one optional piece of a notice: rank says which go last
// when the row is narrow, lowest first.
type notePart struct {
	text string
	rank int
}

// partsLine is a notice of a lead, a tail and optional parts after it.
// Narrow, the lowest-ranked parts go first (the display order stays),
// then the tail, then the lead clips.
func partsLine(lead, tail string, parts []notePart) StatusLine {
	full := lead + tail
	for _, p := range parts {
		full += p.text
	}
	return StatusLine{Text: full, Kind: "text", Fit: func(w int) string {
		kept := append([]notePart(nil), parts...)
		for {
			s := lead + tail
			for _, p := range kept {
				s += p.text
			}
			if cellLen(s) <= w {
				return s
			}
			if len(kept) == 0 {
				break
			}
			low := 0
			for i, p := range kept {
				if p.rank < kept[low].rank {
					low = i
				}
			}
			kept = append(kept[:low], kept[low+1:]...)
		}
		if cellLen(lead) <= w {
			return lead
		}
		return ellipsis(lead, w)
	}}
}

// creditWait is how long a change the poll read waits for your agent's
// write of the file to claim it (docs/ux.md §7b): a hook reports a save
// a moment after the file has it. Unclaimed, it is the plan's.
const creditWait = time.Second

// docChange is a change read from the open plan that the notice row
// hasn't credited yet: the plan before it, and when it was read.
type docChange struct {
	old *plan.Plan
	at  time.Time
}

// lateWait is how long a change announced as the plan's can still be
// claimed by your agent's write of the same bytes: a hook run through a
// login shell (Codex's) can report a save seconds after it.
const lateWait = 10 * time.Second

// lateClaim is a change announced as the plan's that a late hook may
// still credit to your agent: the plan before it, the bytes it read, when,
// and Rex's write it may have saved over.
type lateClaim struct {
	old  *plan.Plan
	hash string
	at   time.Time
	mine *ownEdit
}

// readDoc re-reads the open plan: on the poll's clock, or at once when
// agent says your agent just saved it. Rows, row 1 and drift follow the
// file at once; the notice waits to say who changed it (announce). A
// reload of Rex's own bytes is yours: the key already said it.
func (a *app) readDoc(agent bool) bool {
	if a.doc == nil {
		return false
	}
	old := a.doc.plan
	changed, news := a.doc.poll(a.now())
	if changed {
		a.trackPin()
		a.plans.showDoc(a.doc)
		if a.at == atLibrary {
			a.plans.Rebuild() // Continue's card follows what the task list shows
		}
		if a.at != atTaskDetail {
			// An agent that fills an empty plan lands the cursor on its pin, as
			// an open does (docs/ux.md §1).
			a.outline.keepSel(a.doc.plan)
		}
		a.rejudgeDrift()
	}
	switch {
	case news.text == "":
	case news.own:
		a.flushUnsaid()
		a.mine, a.late = nil, nil
		a.tapeLine("plan", news.text)
		return true
	case a.unsaid == nil:
		a.unsaid, a.late = &docChange{old: old, at: a.now()}, nil
	}
	switch {
	case agent && a.unsaid != nil:
		a.announce(true)
		return true
	case agent && a.late != nil:
		return a.claimLate()
	}
	return changed
}

// claimLate credits to your agent a change already announced as the
// plan's, when your agent's write of those same bytes reports late: the
// notice says so again, as your agent's, and says it saved over Rex's
// write if it did. AGENT's line for it is already there.
func (a *app) claimLate() bool {
	l := a.late
	a.late = nil
	p := a.doc.plan
	if l.hash != a.doc.good || p == nil || a.now().Sub(l.at) > lateWait {
		return false
	}
	switch {
	case a.toastLive() && a.toast.Kind == "error":
	case l.mine.overwritten(a.doc.path, p):
		a.show(l.mine.savedOver())
	default:
		s, done := changeNotice(true, l.old, p, planChanges(l.old, p))
		a.show(s)
		a.toastDone = done
	}
	return true
}

// creditDue announces a change no agent's write claimed in time.
func (a *app) creditDue() bool {
	if a.unsaid == nil || a.now().Before(a.unsaid.at.Add(creditWait)) {
		return false
	}
	a.announce(false)
	return true
}

// flushUnsaid records a change that waited for its credit on AGENT only:
// your own write takes the notice row.
func (a *app) flushUnsaid() {
	u := a.unsaid
	a.unsaid = nil
	if u == nil || u.old == nil || a.doc == nil || a.doc.plan == nil {
		return
	}
	a.tapeLine("plan", planChanges(u.old, a.doc.plan).text)
}

// announce says what the change waiting for its credit did, and who did it
// (docs/ux.md §7b): your agent, when its write of this file claimed it;
// else the plan changed (an editor, a checkout). AGENT gets one line, and
// the notice row the change, unless a refused write's error still holds
// it, or the change only finishes a task's retrospective under its done
// news. A save of your agent's that puts back what Rex wrote over says so,
// once.
func (a *app) announce(agent bool) {
	u := a.unsaid
	a.unsaid = nil
	if u == nil || u.old == nil || a.doc == nil || a.doc.plan == nil {
		return
	}
	p := a.doc.plan
	news := planChanges(u.old, p)
	a.tapeLine("plan", news.text)
	mine := a.mine
	a.mine = nil
	if !agent {
		// Unclaimed for now: a hook that reports late can still claim it.
		a.late = &lateClaim{old: u.old, hash: a.doc.good, at: a.now(), mine: mine}
	}
	switch {
	case a.toastLive() && a.toast.Kind == "error":
	case agent && mine.overwritten(a.doc.path, p):
		a.show(mine.savedOver())
	case !news.tasks && a.toastDone && a.toastLive():
		// The agent finishes a task in several saves (its retrospective,
		// the plan's own status): the done news stays (docs/ux.md §8).
	default:
		s, done := changeNotice(agent, u.old, p, news)
		a.show(s)
		a.toastDone = done
	}
}

// agentLead leads the notice of a change your agent's write claimed.
const agentLead = "Your agent "

// changeNotice is the notice for a change of the plan (docs/ux.md §7b,
// §8), and whether it is done news: your agent finishing the pin (and
// what it pinned next, or what is next up), pinning, unpinning, adding
// tasks or changing the plan; unclaimed, the same done news or what
// changed.
func changeNotice(agent bool, old, now *plan.Plan, news planNews) (StatusLine, bool) {
	was := old.Graph().Pin()
	done := doneToast(was, now)
	if !agent {
		if done.Text != "" {
			return done, true
		}
		return partsLine("The plan changed", " · "+news.text, nil), false
	}
	g := now.Graph()
	pin := g.Pin()
	pinned := pin != nil && pin.Status == plan.Active
	switch {
	case done.Text != "":
		id := flat(was.ID)
		tail := strings.TrimPrefix(done.Text, doneMark+" "+was.ID+" done") // as doneToast words it
		if pinned {
			tail = " and pinned " + flat(pin.ID)
		}
		full := agentLead + "finished " + id + " " + doneMark + tail
		return StatusLine{Text: full, Kind: "text", Fit: func(w int) string {
			if cellLen(full) <= w {
				return full
			}
			return done.Fit(w)
		}}, true
	case pinned && (was == nil || was.ID != pin.ID || was.Status != plan.Active):
		return partsLine(agentLead+"pinned "+flat(pin.ID), "", nil), false
	case was != nil && was.Status == plan.Active && !pinned:
		if t := g.Task(was.ID); t != nil && t.Status == plan.Pending {
			return partsLine(agentLead+"unpinned "+flat(was.ID), "", nil), false
		}
	case news.added > 0 && !news.moved:
		return partsLine(agentLead+"added "+plural(news.added, "%d task", "%d tasks"), "", nil), false
	}
	return partsLine(agentLead+"changed the plan", " · "+news.text, nil), false
}

// statusSegs are the footer's p and d on the task list and a task's
// details (docs/ux.md §4): their verb follows the selection, and each is
// fitted by its longer verb, so moving the cursor never changes the
// footer. None where they can't write: a plan Rex can't read, or a
// read-only one.
func (a *app) statusSegs() []FooterSeg {
	d := a.doc
	if d == nil || d.unwritable() != "" {
		return nil
	}
	t := findTask(d.plan, a.outline.sel)
	if t == nil {
		return nil
	}
	p, dn := seg("p pin", "p unpin"), seg("d done", "d reopen")
	if t.Status == plan.Active && t == planIndexOf(d.plan).pin {
		p.Text = "p unpin"
	}
	if t.Status == plan.Done {
		dn.Text = "d reopen"
	}
	return []FooterSeg{p, dn}
}
