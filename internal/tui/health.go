package tui

import (
	"fmt"
	"time"

	"github.com/luciancheng/rex/internal/skill"
	"github.com/luciancheng/rex/internal/watch"
)

// feedHealth is what the empty tape and Help say about the feed: which
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

// lines is the health block; sk is how the rex-plan skill stands. waiting
// titles it for the empty tape; Help just says "feed".
func (h feedHealth) lines(width int, hidden int, sk skill.Copy, now time.Time, waiting bool) []LogLine {
	var lines []LogLine
	row := func(kind, name, what string) {
		for i, w := range wrap(what, max(8, width-11)) {
			if i > 0 {
				name = ""
			}
			lines = append(lines, LogLine{Kind: kind, Text: "  " + padRight(name, 9) + w})
		}
	}
	title := "feed"
	if waiting {
		title = "feed · waiting for agent events"
	}
	lines = append(lines, LogLine{Kind: "idle", Text: rule(title, width)})
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
