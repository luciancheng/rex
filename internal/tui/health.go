package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// feedHealth is what the empty AGENT tab and Help say about the feed: which
// agents are wired (machine-wide, or for this repo only), whether they
// can learn plans (the rex-plan skill), when the last event came, and
// what Rex hid. The wiring is re-read every few seconds
// while it is on screen, so an install in another terminal shows up; it
// never installs anything.
type feedHealth struct {
	path     string
	repo     string
	last     *watch.Event
	statuses []watch.HarnessStatus
	read     time.Time
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

// refresh re-reads the harness configs once they are healthTTL old.
func (h *feedHealth) refresh(now time.Time) {
	if !h.read.IsZero() && now.Sub(h.read) < healthTTL {
		return
	}
	h.statuses, h.read = watch.HarnessStatusesFor(h.repo), now
}

// saw records the newest feed event, from any repo.
func (h *feedHealth) saw(ev watch.Event) {
	if ev.TS > 0 && (h.last == nil || ev.TS >= h.last.TS) {
		h.last = &ev
	}
}

// lines is the health block under its FEED header (docs/ux.md §10); sk is
// how the rex-plan skill stands. waiting says so in the header, for the
// empty AGENT tab when it has no page title to say it.
func (h feedHealth) lines(width int, hidden int, sk skill.Copy, now time.Time, waiting bool) []LogLine {
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
	meta := ""
	if waiting {
		meta = "waiting for agent events"
	}
	lines = append(lines, h2Line("FEED", meta, width))
	last := "no events yet"
	if h.last != nil {
		last = "last event " + ago(now.Sub(time.Unix(0, int64(h.last.TS*1e9)))) + " (" + h.last.Source + ")"
	}
	row("text", "feed", watch.ShortPath(h.path)+" · "+last)
	for _, s := range h.statuses {
		switch {
		case s.Wired:
			row("text", s.Name, "wired")
		case s.Here:
			row("text", s.Name, "wired (this repo)")
		case s.Present:
			row("text", s.Name, "not wired · rex ingest --install "+s.Name)
		default:
			row("idle", s.Name, "not found")
		}
	}
	// The agent drives the plan only once it has the skill: shown wherever
	// the wiring is, since start opens a plan and skips the Wait screen.
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
