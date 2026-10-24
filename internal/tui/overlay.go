package tui

// overlayKind is what covers a place's body: only Help now, since Rex has
// no actions of its own to ask about (docs/ux.md §1).
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
		head("keys act at once"),
		row("tab", "switch plan ↔ tape"),
		row("↑↓ pgup pgdn", "move, or scroll"),
		row("← →", "back / in: a folder, or a task's detail"),
		row("home end", "first / last; tape: oldest / live"),
		row("⏎", "open the selection, or switch to a plan row 1 offers"),
		row("esc", "back one level (in the plan list, clear first)"),
		row("?", "this screen"),
		row("ctrl+a ctrl+e", "tape: oldest / live"),
		row("ctrl+l", "repaint"),
		row("ctrl+c ×2", "quit"),
		LogLine{Kind: "gap"},
		head("plan list: type, then ⏎"),
		row("text", "find a plan, filter a folder, or name a new plan"),
		row("ctrl+u ctrl+w", "clear the line / delete a word"),
		row("/help", "this screen"),
		row("/breakdance", "one dance"),
		LogLine{Kind: "gap"},
		head("rex never"),
		"runs or approves tools, sends anything to your agent, reads transcripts, or writes your plan (only a new empty one you create in the list). Your agent pins and finishes tasks in the plan file; Rex shows it.",
		LogLine{Kind: "gap"},
		head("outline"),
		row("▸ ○ ·", "pinned / ready / waiting (after another task)"),
		row("✓ – !", "done / skipped / blocked; 1/3 a parent's progress"),
		row("following", "Rex opens each plan file your agent writes"),
		row("latched", "you picked this plan in the list; another the agent writes shows on row 1, ⏎ switches"),
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
