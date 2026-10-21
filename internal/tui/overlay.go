package tui

import "github.com/luciancheng/rex/internal/plan"

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayHelp
	overlayPin
)

// pinAsk is a pin waiting for its confirm: the task, the status the user
// saw, and why it needs asking.
type pinAsk struct {
	id       string
	title    string
	seen     plan.Status
	warnings []string
}

func pinConfirmLines(ask pinAsk, width int) []LogLine {
	lines := []LogLine{{Kind: "command", Text: rule("pin "+ask.id, width)}}
	if ask.title != "" {
		lines = append(lines, LogLine{Kind: "title", Text: "  " + clip(ask.title, width-2)})
	}
	lines = append(lines, LogLine{Kind: "gap"})
	for _, w := range ask.warnings {
		for i, row := range wrap(w, width-4) {
			prefix := "    "
			if i == 0 {
				prefix = "  ! "
			}
			lines = append(lines, LogLine{Kind: "error", Text: prefix + clip(row, width-4)})
		}
	}
	return append(lines, LogLine{Kind: "gap"}, LogLine{Kind: "text", Text: "  " + clip("p⏎ pins it anyway · esc cancels, nothing written", width-2)})
}

// Overlay covers the body of the current place (docs/ux.md §1). Tab leaves
// it open there; Esc closes it.
type Overlay struct {
	Lines  []LogLine
	Footer []string
	Scroll int
}

// helpLines wraps every row to width: at 50+ cells the key sits left of
// its description, narrower it sits above it.
func helpLines(width int) []LogLine {
	var lines []LogLine
	text := func(indent, s string) {
		for _, w := range wrap(s, max(8, width-cellLen(indent))) {
			lines = append(lines, LogLine{Kind: "text", Text: indent + w})
		}
	}
	row := func(key, what string) []LogLine {
		start := len(lines)
		if width >= 50 {
			for i, w := range wrap(what, width-16) {
				lead := "                "
				if i == 0 {
					lead = "  " + padRight(key, 14)
				}
				lines = append(lines, LogLine{Kind: "text", Text: lead + w})
			}
		} else {
			lines = append(lines, LogLine{Kind: "text", Text: "  " + key})
			text("    ", what)
		}
		out := append([]LogLine(nil), lines[start:]...)
		lines = lines[:start]
		return out
	}
	head := func(s string) LogLine { return LogLine{Kind: "idle", Text: "  " + s} }
	rows := []any{
		LogLine{Kind: "command", Text: rule("help", width)},
		head("keys"),
		row("tab", "switch plan ↔ tape"),
		row("↑↓ pgup pgdn", "move, or scroll"),
		row("← →", "back / in: a folder, or a task's detail"),
		row("home end", "first / last; tape: oldest / live"),
		row("⏎", "open, or run what you typed"),
		row("esc", "clear the line, then back one level"),
		row("ctrl+u ctrl+w", "clear the line / delete a word"),
		row("ctrl+a ctrl+e", "tape: oldest / live"),
		row("ctrl+l", "repaint"),
		row("ctrl+c ×2", "quit"),
		LogLine{Kind: "gap"},
		head("type a word, then ⏎"),
		row("? help", "this screen"),
		row("p  pin [id]", "pin the selected task, or next up, or that id"),
		row("breakdance", "one dance"),
		row("/word", "in plans, where typing finds or names, a verb starts with /"),
		row("anything else", "on the tape, a note: local, never sent"),
		LogLine{Kind: "gap"},
		head("rex never"),
		"runs or approves tools, sends anything to your agent, or reads transcripts",
		LogLine{Kind: "gap"},
		head("outline"),
		row("▸ ○ ·", "pinned / ready / waiting (after another task)"),
		row("✓ – !", "done / skipped / blocked; 1/3 a parent's progress"),
	}
	for _, r := range rows {
		switch v := r.(type) {
		case LogLine:
			lines = append(lines, v)
		case []LogLine:
			lines = append(lines, v...)
		case string:
			text("  ", v)
		}
	}
	return lines
}
