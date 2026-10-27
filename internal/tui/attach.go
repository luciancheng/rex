package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// Attach (docs/ux.md §1, §11): Rex opens the plan your agent writes. An
// unlatched session follows each plan file its agent writes in this
// repo's rex/plans; once you pick a plan in the list it is latched, and
// another plan's write is only offered on the notice row, where o opens
// it. Text in the plan list's composer, or a Name prompt, holds an attach
// until it clears.

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
	a.plans.walk = planWalk{} // the list may have a new plan in it
	if a.at == atLibrary {
		a.plans.Rebuild() // so the plan the notice names is in it; the selection stays by key
	}
	if !a.follow {
		a.offer, a.offerName = w, a.wroteName(w)
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
	a.flash("following "+name, "text")
	// An event moved PLAN, so the AGENT tab keeps a record of it (Rex's
	// own line, never the feed).
	a.tapeLine("plan", "following "+name)
	return true
}

// attachBlocked: the plan list's draft has text, or a Name prompt is
// open, in either place (opening a plan would drop them). Help holds
// nothing.
func (a *app) attachBlocked() bool {
	return a.draft != "" || (a.at == atLibrary && a.plans.Prompting())
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

// latch stops following: you picked another plan in the list, or created
// one there (docs/ux.md §11).
func (a *app) latch() { a.follow = false }

// offerHeld makes a write held while you picked or created a plan in the
// list the offer, neither opened nor lost, unless it is the plan you
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
// open plan's screens, on AGENT, and on the plan list's Home while its
// draft is empty, where o would otherwise start a find. Not in Help, nor
// while an attach would be held, since opening the plan would drop the
// plan list's draft or a Name prompt.
func (a *app) offerCan() bool {
	if a.offer == "" || a.overlay[a.pane] != overlayNone || a.attachBlocked() {
		return false
	}
	return a.pane == PaneChat || a.at != atLibrary || a.plans.Screen == ScreenHome
}

// offerStatus is the notice row's new-plan line (docs/ux.md §4): a
// latched session's agent wrote another plan, or a followed write waits
// for what you are typing in the plan list. It stays until taken or until
// you open a plan.
func (a *app) offerStatus() (StatusLine, bool) {
	name, tail, short := a.offerName, "", ""
	switch {
	case a.pending != "" && a.at == atLibrary && a.plans.Prompting():
		// Even an empty Name prompt holds it: only closing the prompt lets go.
		name, tail, short = a.pendingName, " · opens when the prompt closes", " · waiting"
	case a.pending != "":
		name, tail, short = a.pendingName, " · opens when the line is clear", " · waiting"
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
	p := a.planFile(plan.LoadState(a.statePath).Last(a.plans.Repo), "")
	if p == "" {
		p = a.newestPlan()
	}
	if p == "" {
		a.at = atWait
		return
	}
	a.plans.open(p)
	a.openedPlan()
	// An older Rex's skill still teaches that Rex's workflow (the person
	// marks tasks done in Rex), and an upgrader lands here, not on the Wait
	// screen that says so. It wins over the plan list's notice, which the
	// plan list still shows; Help shows the skill too. A missing skill
	// isn't news: Wait and Help say so.
	switch c, n := a.skill.look(a.plans.Repo, a.now()), a.plans.Notice; {
	case c == skill.Stale || c == skill.StaleProject:
		a.flash(skillHint(c), "text")
		a.toast.Short = "rex-plan skill out of date · ? help"
	case n != "":
		// The plan list's notice (outside git, an old rex-plans/) is news
		// at start, which no longer shows the plan list.
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
// for the agent to write one. The headline never goes; under it how a
// plan gets here, the keys, and how to teach the agent the format while
// the rex-plan skill is missing or an older Rex's. The plan list's notice
// is row 1 (§4).
// On a short screen the blank rows go first, then the key rows, then HOW
// IT GOES, then the long skill hint gives way to a short one, which never
// goes: what still doesn't fit scrolls. With plans off it says only that
// and how to turn them on.
func (a *app) waitBody(width, h int) []LogLine {
	if width <= 0 {
		return nil
	}
	inner := max(1, width-2)
	n := a.plans.Notice
	if n == rexFileNotice {
		return appendWrapped(nil, "idle", "A file named rex sits where the rex/plans/ folder goes. Move it to turn plans on; Rex never deletes it.", inner)
	}
	var head []LogLine
	for _, row := range wrap(noPlanText, max(1, inner-2)) {
		head = append(head, spanLine("text", Span{Text: "  "}, Span{Text: row, Bold: true}))
	}
	// Outside git row 1's notice names the folder, so the step keeps the
	// short path.
	how := []LogLine{h2Line("HOW IT GOES", "", width)}
	for i, step := range []string{"you ask your agent to plan the work", "it writes rex/plans/<name>.rex.json", "Rex opens it here and follows it"} {
		for j, row := range wrapPath(step, max(1, width-5)) {
			lead := "     "
			if j == 0 {
				lead = fmt.Sprintf("  %d  ", i+1)
			}
			how = append(how, LogLine{Kind: "text", Text: lead + row})
		}
	}
	var keys []LogLine
	for _, k := range [][2]string{{"esc", "open a plan that already exists"}, {"tab", "watch the agent"}} {
		for j, row := range wrap(k[1], max(1, width-7)) {
			if j > 0 {
				keys = append(keys, LogLine{Kind: "muted", Text: "       " + row})
				continue
			}
			keys = append(keys, spanLine("muted", Span{Text: "  "}, Span{Text: k[0], Role: "text", Bold: true}, Span{Text: "  " + row}))
		}
	}
	long, short := skillLines(a.skill.look(a.plans.Repo, a.now()), width)
	gap := []LogLine{{Kind: "gap"}}
	join := func(parts ...[]LogLine) []LogLine {
		var out []LogLine
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	for _, v := range [][]LogLine{
		join(head, gap, how, gap, keys, gap, long),
		join(head, how, keys, long),
		join(head, how, long),
		join(head, long),
	} {
		if len(v) <= h {
			return v
		}
	}
	return join(head, short) // and if even that doesn't fit, it scrolls
}

// skillLines are the wait screen's hint about the rex-plan skill, muted
// with the command to run in text (docs/ux.md §11): long, on one line
// where it fits and a lead over the command where it doesn't; and short.
// The command wraps rather than clips, so it can still be read. Both are
// nil while every copy agents read is this Rex's.
func skillLines(c skill.Copy, width int) (long, short []LogLine) {
	hint := skillHint(c)
	if hint == "" {
		return nil, nil
	}
	inner := max(1, width-2)
	say, cmd, _ := strings.Cut(hint, " · ")
	command := func(lines []LogLine) []LogLine {
		for _, row := range wrap(cmd, inner) {
			lines = append(lines, LogLine{Kind: "text", Text: "  " + row})
		}
		return lines
	}
	line := func(lead string) LogLine {
		return spanLine("muted", Span{Text: "  " + lead}, Span{Text: cmd, Role: "text"})
	}
	if cellLen(say+" · "+cmd) <= inner {
		long = []LogLine{line(say + " · ")}
	} else {
		leads := []string{say + ":"}
		if c == skill.Missing {
			leads = []string{"agents learn the plan format from the rex-plan skill:", say + ":"}
		}
		// The first lead that fits a row, else the first wrapped with no
		// word left alone above the command.
		w := max(1, inner-2)
		rows := unwidow(wrap(leads[0], w), w)
		for _, lead := range leads {
			if cellLen(lead) <= w {
				rows = []string{lead}
				break
			}
		}
		for _, row := range rows {
			long = append(long, LogLine{Kind: "muted", Text: "  " + row})
		}
		long = command(long)
	}
	verb := "update:"
	if c == skill.Missing {
		verb = "teach it:"
	}
	if cellLen(verb+" "+cmd) <= inner {
		return long, []LogLine{line(verb + " ")}
	}
	return long, command([]LogLine{{Kind: "muted", Text: "  " + verb}})
}

// unwidow moves the next-to-last row's last word down when the last row
// would be one word alone and the pair still fits width.
func unwidow(rows []string, width int) []string {
	n := len(rows)
	if n < 2 || strings.Contains(rows[n-1], " ") {
		return rows
	}
	i := strings.LastIndex(rows[n-2], " ")
	if i < 0 {
		return rows
	}
	if moved := rows[n-2][i+1:] + " " + rows[n-1]; cellLen(moved) <= width {
		rows[n-2], rows[n-1] = rows[n-2][:i], moved
	}
	return rows
}

// skillHint is what to do about the rex-plan skill, or "" when every copy
// agents read is this Rex's.
func skillHint(c skill.Copy) string {
	switch c {
	case skill.Missing:
		return "agents learn plans from the rex-plan skill · rex ingest --install-skill"
	case skill.Stale:
		return "rex-plan skill is out of date · rex ingest --install-skill"
	case skill.StaleProject:
		return "rex-plan skill is out of date · rex ingest --install-skill --project"
	}
	return ""
}

// skillSeen remembers how the rex-plan skill stands where agents read it,
// looked up at most every few seconds: the wait screen is drawn on every
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

// waitKey handles keys on the wait screen, which has no composer: ↑↓ and
// pages scroll a short screen, and Esc or ← go to the plan list.
func (a *app) waitKey(key string) bool {
	if pageKey(&a.outline.scroll, key, a.layout().bodyH) {
		return true
	}
	switch key {
	case "esc", "left":
		return a.toLibrary()
	}
	return false
}

// pageKey scrolls a page that starts at its top (Wait, the empty AGENT
// tab) by key: ↑↓ a row, pages by the body less the two rows ↑ more and
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
