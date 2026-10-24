package tui

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// Attach (docs/ux.md §1, §11): Rex opens the plan your agent writes. An
// unlatched session follows each plan file its agent writes in this
// repo's rex/plans; once you pick a plan in the list, or pin or finish a
// task in it, it is latched, and another plan's write is only offered on
// row 1. Typing in PLAN (except on the Wait screen, whose draft carries
// over), a Name prompt, or a done, send-off or pin overlay holds an
// attach until it clears.

// completedEdit reports whether ev is a file edit that happened: a result,
// not a denied or failed write, never a read (docs/ux.md §8, §9a).
func completedEdit(ev watch.Event) bool {
	ok, has := ev.Meta["ok"].(bool)
	return ev.Kind == "result" && watch.IsFileEditEvent(ev) && (!has || ok)
}

// planFile is p as one of this repo's plan files, or "": an existing
// regular *.rex.json under rex/plans once symlinks are resolved, so a link
// can't lead out. A relative p is taken from cwd. Scope.Rel cleans p, so a
// ".." is judged by where it leads.
func (a *app) planFile(p, cwd string) string {
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		if !filepath.IsAbs(cwd) {
			return ""
		}
		p = filepath.Join(cwd, p)
	}
	rel, in := a.scope.Rel(p)
	if !in || !strings.HasPrefix(rel, "rex/plans/") || !IsPlanFile(path.Base(rel)) {
		return ""
	}
	full := filepath.Join(a.plans.Repo, filepath.FromSlash(rel))
	if info, err := os.Stat(full); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return full
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
	a.plans.walk = planWalk{} // the list may have a new plan in it
	if a.at == atLibrary {
		a.plans.Rebuild() // so the plan row 1 names is in it; the selection stays by key
	}
	if !a.follow {
		a.offer = w
		return true
	}
	a.attach(w)
	return true
}

// attach opens path as the followed plan, or holds it until nothing you
// are doing would be lost. It reports whether anything changed.
func (a *app) attach(path string) bool {
	if a.attachBlocked() {
		changed := a.pending != path
		a.pending = path
		return changed
	}
	a.plans.walk = planWalk{}
	a.plans.open(path)
	a.openedPlan()
	name := a.planLabel(path)
	a.flash("following "+name, "accent")
	// An event moved PLAN, so the tape keeps a record of it (Rex's own
	// line, never the feed).
	a.tapeLine("plan", "following "+name)
	return true
}

// attachBlocked: PLAN's draft has text, a Name prompt is open, or a
// done, send-off or pin overlay is open in either place (opening a plan
// would close it). Help holds nothing, and neither does a draft on the
// Wait screen: it is a verb there as on the outline, so it carries over.
func (a *app) attachBlocked() bool {
	draft := a.draft
	if a.pane != PanePlans {
		draft = a.parked
	}
	if (draft != "" && a.at != atWait) || (a.at == atLibrary && a.plans.Prompting()) {
		return true
	}
	for _, o := range a.overlay {
		if o == overlayPin || o == overlayDone || o == overlaySendoff {
			return true
		}
	}
	return false
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
		return a.attach(f)
	}
	return true
}

// latch stops following: you picked this plan, or wrote to it with p⏎ or
// d⏎ (docs/ux.md §11). A held attach becomes an offer, neither lost nor
// opened.
func (a *app) latch() {
	a.follow = false
	if a.pending != "" {
		a.offer, a.pending = a.pending, ""
	}
}

// takeOffer is ⏎ on an empty draft while row 1 offers a plan: open it
// and follow again. It does nothing where offerTakes says no.
func (a *app) takeOffer() bool {
	if !a.offerTakes() {
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

// offerTakes: ⏎ on an empty draft takes the offer here, which is only
// while row 1 shows it (docs/ux.md §4): not under the plan file's error,
// a toast or hard drift. Not in the Library either, where ⏎ opens the
// selected row, nor in an overlay, nor while this place's draft has text
// (⏎ runs it); and not while an attach would be held, since opening the
// plan would drop PLAN's draft or a form.
func (a *app) offerTakes() bool {
	if _, over := a.overOffer(); over {
		return false
	}
	return a.offerCan()
}

// offerCan is offerTakes without asking what row 1 shows.
func (a *app) offerCan() bool {
	if a.offer == "" || a.overlay[a.pane] != overlayNone || strings.TrimSpace(a.draft) != "" || a.attachBlocked() {
		return false
	}
	return a.pane == PaneChat || a.at != atLibrary
}

// offerStatus is row 1's new-plan line (docs/ux.md §4): a latched
// session's agent wrote another plan, or a followed write waits for what
// you are typing. It stays until taken or until you open a plan.
func (a *app) offerStatus() (StatusLine, bool) {
	p, tail, short := a.offer, "", ""
	switch {
	case a.pending != "":
		p, tail, short = a.pending, " · opens when the line is clear", " · waiting"
	case a.offer == "":
		return StatusLine{}, false
	case a.offerCan():
		tail, short = " · ⏎ switch", " · ⏎ switch"
	}
	name := a.planLabel(p)
	return StatusLine{Text: "new plan " + name + tail, Kind: "accent", Fit: func(w int) string { return fitNewPlan(name, tail, short, w) }}, true
}

// fitNewPlan fits "new plan <name><tail>" into width cells: the name is
// clipped from the left ("…demo") down to headPathMin cells, then short
// takes tail's place, and only then does the name go.
func fitNewPlan(name, tail, short string, width int) string {
	const head = "new plan "
	for _, t := range []string{tail, short} {
		room := width - cellLen(head) - cellLen(t)
		if cellLen(name) <= room {
			return head + name + t
		}
		if room >= headPathMin {
			return head + "…" + tailCells(name, room-1) + t
		}
	}
	return "new plan" + short
}

// planLabel is a plan as row 1 names it: its path under rex/plans,
// without .rex.json.
func (a *app) planLabel(p string) string {
	return strings.ReplaceAll(watch.CleanText(planStem(a.plans.relName(p))), "\n", " ")
}

// followWord is the header's word for how the open plan got there.
func (a *app) followWord() string {
	switch {
	case a.plans.Current == "":
		return ""
	case a.follow:
		return "following"
	}
	return "latched"
}

// newestPlan is this repo's most recently modified plan file, or "".
func (a *app) newestPlan() string {
	for _, p := range walkPlans(a.plans.Root) {
		if f := a.planFile(p.path, ""); f != "" {
			return f
		}
	}
	return ""
}

// land is where start lands (docs/ux.md §11): the plan last opened in this
// repo, else the newest plan, else the wait screen. Either plan is
// followed.
func (a *app) land() {
	p := a.planFile(plan.LoadState(a.statePath).Repos[a.plans.Repo].Plan, "")
	if p == "" {
		p = a.newestPlan()
	}
	if p == "" {
		a.at = atWait
		return
	}
	a.plans.open(p)
	a.openedPlan()
	if n := a.plans.Notice; n != "" {
		// The Library's notice (outside git, an old rex-plans/) is news at
		// start, which no longer shows the Library.
		a.flash(n, "muted")
		a.toast.Fit = func(w int) string { return fitNotice(n, w) }
	}
}

// scanPlans is the cold-start fallback: while no plan is open, a plan file
// that shows up without a hook (an editor save, a missed event) is
// followed too, checked once a poll. Once a plan is open only a write is
// followed; the folder is never scanned to change the screen.
func (a *app) scanPlans() bool {
	if a.plans.Current != "" || a.now().Sub(a.scanned) < docPollEvery {
		return false
	}
	a.scanned = a.now()
	if p := a.newestPlan(); p != "" {
		return a.attach(p)
	}
	return false
}

// waitBody is PLAN before any plan is open (docs/ux.md §11): Rex waits
// for the agent to write one. With plans off it says only that and how
// to turn them on; outside git it names the folder plans go to.
func (a *app) waitBody(width int) []LogLine {
	if width <= 0 {
		return nil
	}
	inner := max(1, width-2)
	lines := []LogLine{{Kind: "command", Text: rule("plan", width)}}
	n := a.plans.Notice
	if n == rexFileNotice {
		lines = append(appendWrapped(lines, "text", n, inner), LogLine{Kind: "gap"})
		return appendWrapped(lines, "idle", "A file named rex sits where the rex/plans/ folder goes. Move it to turn plans on; Rex never deletes it.", inner)
	}
	lines = appendWrapped(lines, "text", noPlanText, inner)
	if n != "" {
		lines = append(lines, LogLine{Kind: "idle", Text: "  " + clip(fitNotice(n, inner), inner)})
	}
	where := "rex/plans/"
	if !a.plans.inGit {
		where = homePath(a.plans.Root) + "/"
	}
	lines = append(lines, LogLine{Kind: "gap"})
	lines = appendWrapped(lines, "idle", "Rex opens "+where+"<name>.rex.json here as soon as your agent writes it.", inner)
	return appendWrapped(lines, "idle", "rex ingest --install-skill teaches agents the plan format.", inner)
}

// waitFooter is the wait screen's footer (docs/ux.md §4); more says the
// copy runs past the screen.
func (a *app) waitFooter(more bool) []string {
	if pv := a.preview(); pv != "" {
		return []string{"no plan", pv}
	}
	if more {
		return []string{"no plan", "esc plans", "tab tape", "↑↓ scroll", "?⏎ help"}
	}
	return []string{"no plan", "esc plans", "tab tape", "?⏎ help"}
}

// waitKey handles keys on the wait screen: verbs, ↑↓ and pages to scroll
// a short screen, and Esc or ← to the plan list.
func (a *app) waitKey(key string) bool {
	step := max(1, layoutFor(a.columns, a.lines).bodyH-2)
	switch key {
	case "up", "down", "pageup", "pagedown":
		delta := map[string]int{"up": -1, "down": 1, "pageup": -step, "pagedown": step}[key]
		a.outline.scroll = max(0, a.outline.scroll+delta) // clamped when drawn
		return true
	case "home":
		a.outline.scroll = 0
		return true
	case "end":
		a.outline.scroll = 1 << 30
		return true
	case "esc", "left":
		if a.draft != "" {
			if key == "left" {
				return false
			}
			a.draft = ""
			return true
		}
		return a.toLibrary()
	case "\r", "\n":
		text := strings.TrimSpace(a.draft)
		if text == "" {
			return false
		}
		if !a.submit(text) {
			a.flash("'"+text+"' is not a verb · ?⏎ help", "error")
		}
		return true
	}
	return false
}
