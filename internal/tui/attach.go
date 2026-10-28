package tui

import (
	"os"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// Attach (docs/ux.md §1, §11): Rex opens the plan your agent writes. An
// unlatched session follows each plan file its agent writes in this
// repo's rex/plans; once you pick a plan on Plans other than by Continue
// or Open latest it is latched, and another plan's write is only offered
// on the notice row, where o opens it. Text in Plans' field, New plan, or
// a task's details or About hold an attach until you are done there.

// completedEdit reports whether ev is a file edit that happened: a result,
// not a denied or failed write, never a read (docs/ux.md §9a, §11).
func completedEdit(ev watch.Event) bool {
	ok, has := ev.Meta["ok"].(bool)
	return ev.Kind == "result" && watch.IsFileEditEvent(ev) && (!has || ok)
}

// planFile is p as one of this repo's plan files, or "" (plan.Jail): a
// relative p is taken from cwd, and a link can't lead out of rex/plans.
func (a *app) planFile(p, cwd string) string {
	return plan.Jail(a.scope, a.plans.Repo, p, cwd)
}

// samePlan reports whether p and q are the same plan file. It goes by the
// file, not the spelling: another case (macOS, Windows) or a link inside
// rex/plans names the same plan.
func samePlan(p, q string) bool {
	if p == "" || q == "" {
		return false
	}
	if p == q {
		return true
	}
	pi, err := os.Stat(p)
	if err != nil {
		return false
	}
	qi, err := os.Stat(q)
	return err == nil && os.SameFile(pi, qi)
}

// isOpen reports whether p is the open plan.
func (a *app) isOpen(p string) bool { return samePlan(p, a.plans.Current) }

// sawWrite notes a completed edit of one of this repo's plan files: of the
// open plan, or of another, the last of which in a batch wins.
func (a *app) sawWrite(ev watch.Event) {
	if !completedEdit(ev) {
		return
	}
	cwd, _ := ev.Meta["cwd"].(string)
	for _, p := range watch.EventPaths(ev) {
		switch f := a.planFile(p, cwd); {
		case f == "":
		case a.isOpen(f):
			a.wroteOpen = true // never hides another plan written with it
		default:
			a.wrote = f
		}
	}
}

// writesOpen reports whether ev is a completed edit of the open plan.
func (a *app) writesOpen(ev watch.Event) bool {
	if !completedEdit(ev) {
		return false
	}
	cwd, _ := ev.Meta["cwd"].(string)
	for _, p := range watch.EventPaths(ev) {
		if f := a.planFile(p, cwd); f != "" && a.isOpen(f) {
			return true
		}
	}
	return false
}

// planWritten acts on the plan writes of the batch just ingested: a write
// of the open plan drops a held attach (the agent is back on it); another
// plan is opened while following, and offered while latched.
func (a *app) planWritten() bool {
	w, open := a.wrote, a.wroteOpen
	a.wrote, a.wroteOpen = "", false
	changed := open && a.pending != ""
	if open {
		a.pending = ""
	}
	if w == "" {
		return changed
	}
	a.plans.walk = planWalk{} // Plans may have a new plan to show
	if a.at == atLibrary {
		a.plans.Rebuild() // so Open latest names it; the selection stays by key
	}
	if !a.follow {
		a.offer, a.offerName = w, a.wroteName(w)
		return true
	}
	a.attach(w)
	return true
}

// attach opens path, which your agent wrote, as the followed plan, or
// holds it until nothing you are doing would be lost. It reports whether
// anything changed.
func (a *app) attach(path string) bool {
	if a.attachBlocked() {
		changed := a.pending != path
		if changed {
			a.pendingName = a.wroteName(path)
		}
		a.pending = path
		return changed
	}
	a.plans.walk = planWalk{}
	a.plans.open(path)
	a.openedPlan()
	name := a.planLabel(path)
	title, rel := a.planTitle(), a.plans.CurrentRel()
	if title == "" {
		title = name
	}
	a.show(openNotice(wrotePrefix+title+" · opened "+rel, wrotePrefix, rel))
	// An event moved PLAN, so the AGENT tab keeps a record of it (Rex's
	// own line, never the feed).
	a.tapeLine("plan", "following "+name)
	return true
}

// attachBlocked: Plans' field holds text or New plan is open (opening a
// plan would drop them), or you are reading a task's details or About
// (docs/ux.md §11), in either place. Help holds nothing.
func (a *app) attachBlocked() bool {
	return a.fieldHolds() || a.reading()
}

// fieldHolds: Plans' field has text, or New plan is open.
func (a *app) fieldHolds() bool {
	return a.draft != "" || (a.at == atLibrary && a.plans.Prompting())
}

// reading: PLAN shows a task's details or About.
func (a *app) reading() bool {
	return a.at == atTaskDetail || a.at == atPlanDetail
}

// applyPending opens a held attach once nothing blocks it (settle). A held
// plan that is gone, or is the open plan by now, just stops being held.
func (a *app) applyPending() bool {
	if a.pending == "" || a.attachBlocked() {
		return false
	}
	p := a.pending
	a.pending = ""
	if f := a.planFile(p, ""); f != "" && !a.isOpen(f) {
		return a.attach(f) // only a write is ever held
	}
	return true
}

// latch stops following: you picked a plan on Plans other than by
// Continue or Open latest, or created one there (docs/ux.md §11).
func (a *app) latch() { a.follow = false }

// offerHeld makes a write held while you picked or created a plan on
// Plans the offer, neither opened nor lost, unless it is the plan you
// picked (docs/ux.md §11). openedPlan clears pending, so callers save it
// first.
func (a *app) offerHeld(held string) {
	if held != "" && !a.isOpen(held) {
		a.offer, a.offerName = held, a.wroteName(held)
	}
}

// takeOffer is o while the notice row offers a plan: open it and follow
// again. It does nothing where offerOpens says no.
func (a *app) takeOffer() bool {
	if !a.offerOpens() {
		return false
	}
	p := a.offer
	a.offer = ""
	f := a.planFile(p, "")
	if f == "" {
		a.flash(a.planLabel(p)+" is gone", "error")
		return true
	}
	if !a.isOpen(f) {
		a.plans.open(f)
		a.openedPlan()
	}
	a.follow = true
	return true
}

// offerKey reports that key is o taking the offer now.
func (a *app) offerKey(key string) bool {
	return key == "o" && a.offerOpens()
}

// offerOpens: o opens the offer here, which is only while the notice row
// shows it and names o (docs/ux.md §4): not under an error, your agent's
// question or a toast.
func (a *app) offerOpens() bool {
	if !a.offerCan() {
		return false
	}
	_, kind := a.noticeOf()
	return kind == noticeOffer
}

// offerCan is offerOpens without asking what the notice row shows: on the
// open plan's screens, details and About included, on AGENT, and on Plans
// while its field is empty, where o would otherwise start a find. Not in
// Help, nor while Plans' field holds text or New plan is open, since
// opening the plan would drop them.
func (a *app) offerCan() bool {
	if a.offer == "" || a.overlay[a.pane] != overlayNone || a.fieldHolds() {
		return false
	}
	return a.pane == PaneChat || a.at != atLibrary || a.plans.Screen == ScreenHome
}

// offerStatus is the notice row's new-plan line (docs/ux.md §4): a
// latched session's agent wrote another plan, or a followed write waits
// for what you are typing on Plans or reading. It stays until taken or
// until you open a plan.
func (a *app) offerStatus() (StatusLine, bool) {
	name, tail, short := a.offerName, "", ""
	switch {
	case a.pending != "" && a.at == atLibrary && a.plans.Prompting():
		// Even an empty New plan holds it: only closing it lets go.
		name, tail, short = a.pendingName, " · opens when the prompt closes", " · waiting"
	case a.pending != "" && a.draft != "":
		name, tail, short = a.pendingName, " · opens when the field is clear", " · waiting"
	case a.pending != "":
		name, tail, short = a.pendingName, " · opens when you're back on the list", " · waiting"
	case a.offer == "":
		return StatusLine{}, false
	case a.offerCan():
		tail, short = " · o opens it", " · o opens it"
	}
	return StatusLine{Text: wrotePrefix + name.full() + tail, Kind: "text", Fit: func(w int) string { return fitWrote(name, tail, short, w) }}, true
}

// wrotePrefix leads the notice of a plan your agent wrote.
const wrotePrefix = "Your agent wrote "

// wroteAs is a plan the agent wrote as the notice row names it: its
// title, if it has one, and its path under rex/plans without .rex.json.
type wroteAs struct{ title, path string }

func (n wroteAs) full() string {
	if n.title == "" || n.title == n.path {
		return n.path
	}
	return n.title + " (" + n.path + ")"
}

// wroteName reads the plan the agent wrote once, for its notice.
func (a *app) wroteName(p string) wroteAs {
	n := wroteAs{path: a.planLabel(p)}
	if got, err := plan.Load(p); err == nil {
		n.title = strings.TrimSpace(flat(got.Title))
	}
	return n
}

// headPathMin is the fewest cells of a plan's path a notice keeps: "…demo".
const headPathMin = 5

// fitWrote fits "Your agent wrote <title> (<path>)<tail>" into width
// cells: the title goes first, then the path is clipped from the left
// ("…demo") down to headPathMin cells, then short takes tail's place,
// then "new plan" stands in for the lead.
func fitWrote(n wroteAs, tail, short string, width int) string {
	for _, head := range []string{wrotePrefix, "new plan "} {
		for _, t := range []string{tail, short} {
			if s := head + n.full() + t; cellLen(s) <= width {
				return s
			}
			room := width - cellLen(head) - cellLen(t)
			if cellLen(n.path) <= room {
				return head + n.path + t
			}
			if room >= headPathMin {
				return head + "…" + tailCells(n.path, room-1) + t
			}
		}
	}
	return "new plan" + short
}

// planLabel is a plan as the notice row names it: its path under rex/plans,
// without .rex.json.
func (a *app) planLabel(p string) string {
	return strings.ReplaceAll(watch.CleanText(planStem(a.plans.relName(p))), "\n", " ")
}

// land is where start lands (docs/ux.md §11): Plans, every time, with
// Continue the plan last opened in this repo (~/.rex/state.json), if it is
// still one of its plans. Nothing opens until you pick a plan or your
// agent writes one. An older Rex's skill still teaches that Rex's
// workflow, so with plans to open it is the notice row's news at start; a
// first run says so under the Create a plan card, and Help shows it too.
func (a *app) land() {
	b := a.plans
	if b.Continue == "" {
		b.Continue = b.Current
	}
	if b.Continue == "" {
		b.Continue = a.planFile(plan.LoadState(a.statePath).Last(b.Repo), "")
	}
	b.Screen, b.fresh = ScreenHome, true
	b.Rebuild()
	if b.total == 0 {
		return // the first run says it under the Create a plan card
	}
	if c := a.skill.look(b.Repo, a.now()); c == skill.Stale || c == skill.StaleProject {
		a.flash(skillHint(c), "text")
		a.toast.Short = "rex-plan skill out of date · ? help"
	}
}

// scanPlans refreshes Plans from the folder once a poll while it is on
// screen (docs/ux.md §11): Open latest, the counts and Recent, so a plan
// that shows up without a hook (an editor save) is one card away. It never
// opens a plan: only your agent's write does (attach).
func (a *app) scanPlans() bool {
	if a.at != atLibrary || a.now().Sub(a.scanned) < docPollEvery {
		return false
	}
	a.scanned = a.now()
	keep := len(a.plans.Rows)
	a.plans.walk = planWalk{}
	a.plans.Rebuild()
	return len(a.plans.Rows) != keep || a.pane == PanePlans
}

// skillSeen remembers how the rex-plan skill stands where agents read it,
// looked up at most every few seconds: Plans' first run is drawn on every
// frame, and an install in another terminal still shows.
type skillSeen struct {
	at  time.Time
	got skill.Copy
}

func (s *skillSeen) look(repo string, now time.Time) skill.Copy {
	if !s.at.IsZero() && now.Sub(s.at) < 3*time.Second && !now.Before(s.at) {
		return s.got
	}
	home, _ := os.UserHomeDir()
	s.at, s.got = now, skill.Look(home, repo)
	return s.got
}

// pageKey scrolls a page that starts at its top (a plan with no list, the
// empty AGENT tab) by key: ↑↓ a row, pages by the body less the two rows ↑ more and
// ↓ more take, Home and End to its ends. at is clamped when drawn.
func pageKey(at *int, key string, bodyH int) bool {
	step := max(1, bodyH-2)
	switch key {
	case "up", "down", "pageup", "pagedown":
		delta := map[string]int{"up": -1, "down": 1, "pageup": -step, "pagedown": step}[key]
		*at = max(0, *at+delta)
	case "home":
		*at = 0
	case "end":
		*at = 1 << 30
	default:
		return false
	}
	return true
}
