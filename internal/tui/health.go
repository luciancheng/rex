package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// feedHealth is what the empty AGENT tab and Help say about the feed: which
// agents are wired (machine-wide, or for this repo only) and whether they
// get the pin, whether they can learn plans (the rex-plan skill), when the
// last event came, and what Rex hid. The wiring is re-read every few
// seconds while it is on screen, or while a plan's send state rests on
// it, so an install in another terminal shows up; it never installs
// anything.
type feedHealth struct {
	path     string
	repo     string
	last     *watch.Event
	statuses []watch.HarnessStatus
	read     time.Time
	// statusesFor reads the wiring; nil is watch.HarnessStatusesFor. A test
	// pins it.
	statusesFor func(repo string) []watch.HarnessStatus
}

const healthTTL = 2 * time.Second

func readHealth(path, repo string, now time.Time) feedHealth {
	h := feedHealth{path: path, repo: repo}
	if recent := watch.LastEvents(path, 1); len(recent) == 1 {
		h.last = &recent[0]
	}
	h.refresh(now)
	return h
}

// refresh re-reads the harness configs once they are healthTTL old, or
// the clock has gone back past when they were read.
func (h *feedHealth) refresh(now time.Time) {
	if !stale(h.read, now) {
		return
	}
	read := h.statusesFor
	if read == nil {
		read = watch.HarnessStatusesFor
	}
	h.statuses, h.read = read(h.repo), now
}

// saw records the newest feed event, from any repo.
func (h *feedHealth) saw(ev watch.Event) {
	if ev.TS > 0 && (h.last == nil || ev.TS >= h.last.TS) {
		h.last = &ev
	}
}

// lines is the health block under its FEED header (docs/ux.md §10); sk is
// how the rex-plan skill stands.
func (h feedHealth) lines(width int, hidden int, sk skill.Copy, now time.Time) []LogLine {
	var lines []LogLine
	// A row's words wrap in the value column. One wider than the column (a
	// flag, a path) gets a row of its own, hung at column 4, whole: a
	// command split mid-word can't be copied.
	row := func(kind, name, what string) {
		col, line := max(8, width-11), ""
		flush := func() {
			lines = append(lines, LogLine{Kind: kind, Text: strings.TrimRight("  "+padRight(name, 9)+line, " ")})
			name, line = "", ""
		}
		for _, w := range strings.Fields(what) {
			switch {
			case cellLen(w) > col:
				if line != "" || name != "" {
					flush()
				}
				for _, part := range hardWrap(w, max(1, width-4)) { // split only when wider than the row
					lines = append(lines, LogLine{Kind: kind, Text: "    " + part})
				}
			case line == "":
				line = w
			case cellLen(line)+1+cellLen(w) <= col:
				line += " " + w
			default:
				flush()
				line = w
			}
		}
		if line != "" || name != "" {
			flush()
		}
	}
	lines = append(lines, h2Line("FEED", "", width))
	last := "no events yet"
	if h.last != nil {
		last = "last event " + ago(now.Sub(time.Unix(0, int64(h.last.TS*1e9)))) + " (" + h.last.Source + ")"
	}
	row("text", "feed", watch.ShortPath(h.path)+" · "+last)
	for _, s := range h.statuses {
		row(pinRow(s))
	}
	// The agent drives the plan only once it has the skill: shown wherever
	// the wiring is, since Plans says so only on a first run.
	switch sk {
	case skill.Current:
		row("text", "skill", "rex-plan current")
	case skill.Missing:
		row("text", "skill", "missing · rex ingest --install-skill")
	case skill.Stale:
		row("text", "skill", "out of date · rex ingest --install-skill")
	case skill.StaleProject:
		row("text", "skill", "out of date · rex ingest --install-skill --project")
	}
	if hidden > 0 {
		noun := "events"
		if hidden == 1 {
			noun = "event"
		}
		for _, w := range wrap(fmt.Sprintf("%d %s from other folders hidden", hidden, noun), max(8, width-2)) {
			lines = append(lines, LogLine{Kind: "idle", Text: "  " + w})
		}
	}
	return lines
}

// pinRow is a harness's row of feed health (docs/ux.md §10): whether it
// is wired, and whether that hands your agent the pin (§7a), or the one
// command that makes it. Its words come from the harness's status, never
// its name: a plugin from before the pin (Old); a hook on no event that
// hands it over, or not on all of them (Prompts, Partial); what holds for
// it (Caveat).
func pinRow(s watch.HarnessStatus) (kind, name, what string) {
	install, where := "rex ingest --install "+s.Name, "wired"
	if s.Here && !s.Wired {
		install, where = install+" --project", "wired for this repo"
	}
	sends := "sends the pin with each prompt"
	if s.Caveat != "" {
		sends += " " + s.Caveat
	}
	switch {
	case !s.Wired && !s.Here && s.Present:
		return "text", s.Name, "not wired · " + install
	case !s.Wired && !s.Here:
		return "idle", s.Name, "not found"
	case s.Old:
		return "text", s.Name, where + " · old plugin, no pin: " + install
	case !s.Prompts || s.Partial:
		return "text", s.Name, where + " · rerun " + install
	}
	return "text", s.Name, where + " · " + sends
}

// feedLead is the empty AGENT tab's row 1 (docs/ux.md §4, §10); narrow,
// feedShort.
const (
	feedLead  = "Rex shows your agent's work here as its hooks report it."
	feedShort = "Rex shows your agent's work here."
)

// emptyAgent is AGENT before any event: the feed's health (docs/ux.md
// §10). Its lead, what the tab is for, is row 1 (§4).
func (a *app) emptyAgent(width int) []LogLine {
	a.health.refresh(a.now())
	return a.health.lines(width, a.hidden, a.skill.look(a.plans.Repo, a.now()), time.Now())
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
