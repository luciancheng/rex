package tui

import (
	"fmt"
	"time"

	"github.com/luciancheng/rex/internal/watch"
)

// feedHealth is what the empty tape and Help say about the feed: which
// agents are wired, when the last event came, and what Rex hid. It is read
// once at start and never installs anything.
type feedHealth struct {
	path     string
	last     *watch.Event
	statuses []watch.HarnessStatus
}

func readHealth(path string) feedHealth {
	h := feedHealth{path: path, statuses: watch.HarnessStatuses()}
	if recent := watch.LastEvents(path, 1); len(recent) == 1 {
		h.last = &recent[0]
	}
	return h
}

func (h feedHealth) lines(width int, hidden int, now time.Time) []LogLine {
	row := func(name, what string) LogLine {
		return LogLine{Kind: "text", Text: "  " + padRight(name, 9) + what}
	}
	lines := []LogLine{{Kind: "idle", Text: rule("feed · waiting for agent events", width)}}
	last := "no events yet"
	if h.last != nil {
		last = "last event " + ago(now.Sub(time.Unix(0, int64(h.last.TS*1e9)))) + " (" + h.last.Source + ")"
	}
	lines = append(lines, row("feed", watch.ShortPath(h.path)+" · "+last))
	for _, s := range h.statuses {
		switch {
		case s.Wired:
			lines = append(lines, row(s.Name, "wired"))
		case s.Present:
			lines = append(lines, row(s.Name, "not wired · rex ingest --install "+s.Name))
		default:
			lines = append(lines, LogLine{Kind: "idle", Text: "  " + padRight(s.Name, 9) + "not found"})
		}
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
