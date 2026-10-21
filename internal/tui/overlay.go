package tui

import "github.com/luciancheng/rex/internal/watch"

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
)

// Overlay covers the body of the current place (docs/ux.md §1). Tab leaves
// it open there; Esc closes it.
type Overlay struct {
	Lines  []LogLine
	Footer []string
	Scroll int
}

func helpLines(width int, feed string) []LogLine {
	row := func(key, what string) LogLine {
		return LogLine{Kind: "text", Text: "  " + padRight(key, 14) + what}
	}
	head := func(s string) LogLine { return LogLine{Kind: "idle", Text: "  " + s} }
	lines := []LogLine{
		{Kind: "command", Text: rule("help", width)},
		head("keys"),
		row("tab", "switch plan ↔ tape"),
		row("↑↓ pgup pgdn", "move, or scroll"),
		row("⏎", "open, or run what you typed"),
		row("esc", "clear the line, then back one level"),
		row("ctrl+u ctrl+w", "clear the line / delete a word"),
		row("ctrl+a ctrl+e", "tape: oldest / live"),
		row("ctrl+l", "repaint"),
		row("ctrl+c ×2", "quit"),
		{Kind: "gap"},
		head("type a word, then ⏎"),
		row("? help", "this screen"),
		row("breakdance", "one dance"),
		row("anything else", "a note on the tape, local, never sent"),
		{Kind: "gap"},
		head("rex never"),
		{Kind: "text", Text: "  runs or approves tools, sends anything to your agent, or reads transcripts"},
		{Kind: "gap"},
		head("feed"),
		{Kind: "text", Text: "  " + watch.ShortPath(feed)},
	}
	return lines
}
