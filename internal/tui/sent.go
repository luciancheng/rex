package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/plan"
	"github.com/luciancheng/rex/internal/watch"
)

// The pin's send state (docs/ux.md §4, §7a): whether what the open plan
// hands your agent went with your last prompt, goes with your next one, or
// can't go. It is read from the feed (the prompt's event says what it
// carried and from which repo, stamped by rex ingest), the harnesses'
// wiring and state.json, so Rex keeps no state file for it, and it never
// claims more than happened: only a prompt stamped with this repo, since
// the pin last changed, counts as sent.
const (
	sentLast  = "sent with last prompt"
	sendsNext = "sends with next prompt"
	notSent   = "not sent"
	noHarness = "not sent · ? help"
)

// sendSeen is what the send state rests on besides the open plan.
type sendSeen struct {
	prompt *promptSeen // the newest prompt for this repo, nil before one

	repo os.FileInfo // this repo's folder, to tell its stamps (pin_repo) by file

	// key is what the open plan sends, as trackPin last saw it: a change
	// (another pin, none, another plan) makes the last prompt older than
	// it. Memory only.
	key   string
	keyed bool

	// last is state.json's last plan for this repo, through the jail: only
	// it sends anything (docs/ux.md §7a). Re-read on healthTTL.
	last   string
	read   time.Time
	lastOf string // memo: the open plan last compared with last
	isLast bool

	plan *plan.Plan // memo: the copy of the open plan what was read from
	path string
	full string // path through the jail, "" when it leads out of rex/plans
	what *plan.Pinned
}

// promptSeen is one prompt for this repo, and what it carried: only rex
// ingest stamps that, and only stamps that name this repo count.
type promptSeen struct {
	here   bool   // stamped with this repo (pin_repo): its note was this repo's
	unread bool   // and its harness's build doesn't read the note (pin_unread)
	before bool   // the pin or plan changed since (trackPin): it can't have carried the one now
	pin    string // the task's id, "" for a new plan's line
	file   string // the plan it named, a file of this repo's, or ""
	memo   string // the open plan last compared with file
	same   bool
}

// stampedHere reports whether rex ingest stamped ev with this repo: the
// repo its note was looked up in (pin_repo) is this one, by file. A
// prompt from a folder above or inside the repo, or from a workspace with
// more roots, may be this repo's by where it came from and still carry
// another repo's pin.
func (a *app) stampedHere(ev watch.Event) bool {
	repo, _ := ev.Meta["pin_repo"].(string)
	if !filepath.IsAbs(repo) {
		return false
	}
	s := &a.sent
	if s.repo == nil {
		info, err := os.Stat(a.plans.Repo)
		if err != nil {
			return false
		}
		s.repo = info
	}
	info, err := os.Stat(repo)
	return err == nil && os.SameFile(info, s.repo)
}

// sawPrompt notes a prompt for this repo, in order: the newest wins. One
// stamped with this repo (here) says what it carried; any other (from this
// repo's agent unstamped, or with another repo's stamps, or from nowhere
// known) carried nothing of this repo's.
func (a *app) sawPrompt(ev watch.Event, here bool) {
	p := &promptSeen{here: here}
	if here {
		p.pin, _ = ev.Meta["pin"].(string)
		p.unread, _ = ev.Meta["pin_unread"].(bool)
		if rel, _ := ev.Meta["pin_plan"].(string); rel != "" && !filepath.IsAbs(rel) {
			p.file = a.planFile(filepath.Join(a.plans.Repo, filepath.FromSlash(rel)), "")
		}
	}
	a.sent.prompt = p
}

// unstamped is ev without what it carried, for AGENT: another repo's pin
// is never drawn as this repo's.
func unstamped(ev watch.Event) watch.Event {
	if ev.Meta["pin"] == nil && ev.Meta["pin_plan"] == nil {
		return ev
	}
	meta := make(map[string]any, len(ev.Meta))
	for k, v := range ev.Meta {
		if k != "pin" && k != "pin_plan" {
			meta[k] = v
		}
	}
	ev.Meta = meta
	return ev
}

// carried reports whether the prompt carried what (the open plan's pin,
// or its name with no tasks yet), the plan at path, since it last changed:
// by file, never by spelling.
func (p *promptSeen) carried(what *plan.Pinned, path string) bool {
	if p == nil || !p.here || p.before || p.file == "" {
		return false
	}
	id := ""
	if what.Task != nil {
		id = what.Task.ID
	}
	if p.pin != id {
		return false
	}
	if p.memo != path {
		p.memo, p.same = path, samePlan(p.file, path)
	}
	return p.same
}

// trackPin notes what the open plan sends now (docs/ux.md §7a): when it
// differs from what it sent when last seen (you pinned or unpinned, your
// agent moved the pin, another plan opened), the newest prompt is older
// than it, so it no longer counts as sent, until the next one. The first
// plan opened only sets it, so a prompt from before Rex started counts.
func (a *app) trackPin() {
	key := ""
	if a.doc != nil {
		key = a.doc.path + "\x00-"
		if what := a.sendWhat(); what != nil {
			key = a.doc.path + "\x00+"
			if what.Task != nil {
				key += what.Task.ID
			}
		}
	}
	s := &a.sent
	if s.keyed && key != s.key && s.prompt != nil {
		s.prompt.before = true
	}
	s.key, s.keyed = key, true
}

// sendWhat is what a prompt would carry from the open plan's copy Rex
// shows (plan.Sends): its pin, or its name with no tasks yet; nil when it
// sends nothing. A file whose links lead out of rex/plans sends nothing.
func (a *app) sendWhat() *plan.Pinned {
	d, s := a.doc, &a.sent
	if d == nil || d.plan == nil {
		return nil
	}
	if s.plan != d.plan || s.path != d.path {
		s.plan, s.path, s.what = d.plan, d.path, nil
		if s.full = a.planFile(d.path, ""); s.full != "" {
			if rel, err := filepath.Rel(a.plans.Repo, s.full); err == nil {
				s.what = plan.Sends(filepath.ToSlash(rel), d.plan)
			}
		}
	}
	return s.what
}

// sendState is the open plan's send state, or "" with no plan to show
// (docs/ux.md §4, §7a): sent with last prompt when the newest prompt for
// this repo carried it, stamped with this repo, since the pin last
// changed; else not sent when the plan sends nothing (nothing pinned,
// read-only) or can't now (unsent); else not sent · ? help when no wired
// harness hands it over, or the newest prompt's harness can't read it;
// else sends with next prompt.
func (a *app) sendState() string {
	if a.doc == nil || a.doc.plan == nil {
		return ""
	}
	what := a.sendWhat()
	switch {
	case what == nil:
		return notSent
	case a.sent.prompt.carried(what, a.doc.path):
		return sentLast
	}
	if why := a.unsent(); why != "" {
		return why
	}
	return sendsNext
}

// unsent is why the open plan's pin can't go with the next prompt, as the
// send state says it, or "" when it can: the hook can't read the file
// now, its links lead out of rex/plans, it isn't the plan Rex last opened
// here, or the note can't name its path (not sent); no wired harness
// hands it over, or the newest prompt for this repo came from a build that
// doesn't read it (not sent · ? help).
func (a *app) unsent() string {
	if a.whyNot() != "" {
		return notSent
	}
	if p := a.sent.prompt; p != nil && p.here && p.unread || !slices.ContainsFunc(a.health.statuses, watch.HarnessStatus.Sends) {
		return noHarness
	}
	return ""
}

// whyNot is why the open plan sends nothing whatever is pinned, in words
// for its details, or "".
func (a *app) whyNot() string {
	d := a.doc
	a.sendWhat() // the jailed path, read once per copy of the plan
	full := a.sent.full
	_, bad := docStatus(d)
	rel, _ := filepath.Rel(a.plans.Repo, full)
	switch {
	case d.plan != nil && d.plan.ReadOnly != "":
		return "it is read-only"
	case full == "":
		return "its file isn't under rex/plans/"
	case bad:
		return "Rex can't read the file now"
	case !a.isLast():
		return "it isn't the plan Rex last opened here"
	case !plan.Nameable(filepath.ToSlash(rel)):
		return "its path is too long for the note"
	}
	return ""
}

// isLast reports whether the open plan is state.json's last for this
// repo, the one plan a prompt hook reads.
func (a *app) isLast() bool {
	s := &a.sent
	if s.lastOf != a.doc.path {
		s.lastOf, s.isLast = a.doc.path, samePlan(s.last, a.doc.path)
	}
	return s.isLast
}

// detailNote is the line under a task's title on its details (docs/ux.md
// §6, §7a), what its fields do for your agent, in the send state's words:
// for the pin, whether they went with the last prompt, go with the next,
// or why they don't; for another task, what pinning it would do. "" for a
// done or missing task, whose page says what was done.
func (a *app) detailNote(key string) string {
	d := a.doc
	if d == nil || d.plan == nil {
		return ""
	}
	t := findTask(d.plan, key)
	if t == nil || t.Status == plan.Done {
		return ""
	}
	if d.plan.ReadOnly != "" {
		return "A read-only plan sends your agent nothing."
	}
	if pin := planIndexOf(d.plan).pin; t == pin && t.Status == plan.Active {
		switch a.sendState() {
		case sentLast:
			return "Your agent got these fields with your last prompt."
		case sendsNext:
			return "These fields go to your agent with your next prompt."
		case noHarness:
			if p := a.sent.prompt; p != nil && p.here && p.unread {
				return "Not sent: your agent's build didn't read it with your last prompt · ? help"
			}
			return "Not sent: no wired agent takes the pin · ? help"
		}
		return "This plan sends your agent nothing: " + a.whyNot() + "."
	}
	switch a.unsent() {
	case notSent:
		return "This plan sends your agent nothing: " + a.whyNot() + "."
	case noHarness:
		return "Pin it (p) to send these fields; no wired agent takes them now · ? help"
	}
	return "Pin it (p) and these fields go to your agent with your next prompt."
}

// sendTail is the end of p's notice (docs/ux.md §7b): what the pin will do
// now, in the send state's words.
func (a *app) sendTail() string {
	if a.sendWhat() == nil {
		return " · " + notSent
	}
	if why := a.unsent(); why != "" {
		return " · " + why
	}
	return " · sends with your next prompt"
}

// sentRight is the send state as row 1's right side, muted: on the task
// list, and on the pinned task's details (docs/ux.md §4).
func (a *app) sentRight() []Span {
	if s := a.sendState(); s != "" {
		return []Span{{Text: s, Role: "muted"}}
	}
	return nil
}

// refreshSend re-reads what the send state rests on, once it is healthTTL
// old (or the clock went back past it), while a plan is open: the
// harnesses' wiring and state.json's last plan. It reports whether the
// send state changed, for a repaint.
func (a *app) refreshSend() bool {
	if a.doc == nil || a.doc.plan == nil {
		return false
	}
	now, s := a.now(), &a.sent
	before := a.sendState()
	a.health.refresh(now)
	if stale(s.read, now) {
		s.read, s.lastOf = now, ""
		s.last = a.planFile(plan.LoadState(a.statePath).Last(a.plans.Repo), "")
	}
	return a.sendState() != before
}

// stale reports whether something read at read is due again at now:
// never read, healthTTL old, or read at a time the clock has gone back
// past.
func stale(read, now time.Time) bool {
	return read.IsZero() || now.Before(read) || now.Sub(read) >= healthTTL
}

// lastOpened is the plan Rex just recorded as this repo's last in
// state.json, so the send state needn't wait for a re-read.
func (a *app) lastOpened(path string) {
	a.sent.last, a.sent.lastOf, a.sent.read = path, "", a.now()
}

// carriedTag is what a prompt carried to your agent, as AGENT says under
// it (docs/ux.md §10): "with pin <id>", or for a new plan's line "with
// plan <name>"; "" for neither. It is read from the stamps rex ingest
// put on the event, never from its text; another repo's are gone by then
// (unstamped).
func carriedTag(ev watch.Event) (lead, what string) {
	if id, _ := ev.Meta["pin"].(string); strings.TrimSpace(id) != "" {
		return "with pin ", flat(id)
	}
	if rel, _ := ev.Meta["pin_plan"].(string); strings.TrimSpace(rel) != "" {
		return "with plan ", flat(planStem(strings.TrimPrefix(rel, "rex/plans/")))
	}
	return "", ""
}
