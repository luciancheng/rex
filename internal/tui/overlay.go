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
	Footer []FooterSeg
	Scroll int
}

// helpKeyCol is Help's key column: the key and its padding.
const helpKeyCol = 11

// helpKey is one row of Help: a key (or a mark) and what it does. key
// overrides the key's look, for marks that keep their own colors. With
// neither a name nor a key it is a note under the keys, with no key column.
type helpKey struct {
	name, what string
	key        []Span
}

// pageKeys is THIS PAGE in Help: every key the page it was opened over
// takes, not only its footer's (docs/ux.md §13). enter's switch to row 1's
// offer reads the same on every page that takes it.
var pageKeys = map[string][]helpKey{
	"outline": {
		{name: "enter →", what: "open the selected task; on the title row, about"},
		{name: "enter", what: "switch PLAN to a new plan row 1 offers"},
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last row (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to PLANS"},
	},
	"about": {
		{name: "enter", what: "switch PLAN to a new plan row 1 offers"},
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to the outline"},
	},
	"plans": {
		{name: "enter", what: "open the selected plan, or create one"},
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "type", what: "find a plan across rex/plans"},
		{name: "esc ←", what: "with no plan open, back to waiting"},
	},
	"folder": {
		{name: "enter", what: "open the plan, or go into the folder"},
		{name: "→", what: "into the folder"},
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "type", what: "filter this folder"},
		{name: "esc ←", what: "up a folder; at the top, back to PLANS"},
	},
	"new plan": {
		{name: "enter", what: "create it and open its outline"},
		{name: "type", what: "its name; a/b makes plan b in folder a"},
		{name: "esc", what: "clear the name, else cancel: nothing is written"},
	},
	"new folder": {
		{name: "enter", what: "create it and go into it"},
		{name: "type", what: "its name; a/b makes b inside a"},
		{name: "esc", what: "clear the name, else cancel: nothing is written"},
	},
	"waiting": {
		{name: "esc ←", what: "the plan list: open or create a plan yourself"},
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
	},
	"agent": {
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home", what: "oldest (also ctrl+a)"},
		{name: "end", what: "live (also ctrl+e)"},
		{name: "enter", what: "switch PLAN to a new plan row 1 offers"},
		// No key unfolds a run (docs/ux.md §10), so this is a note, not a key.
		{what: "folding is display only: the feed keeps every thought"},
	},
}

// helpLines is Help (docs/ux.md §13): THIS PAGE, for the page it opened over,
// then what holds everywhere, the marks, how AGENT names its blocks, the
// plan list's typing and what Rex never does; the feed's health follows.
// tall puts a blank row above each section. From 34 cells the key sits in
// an 11-cell column left of what it does; narrower it sits above it.
func helpLines(width int, page string, tall bool) []LogLine {
	var lines []LogLine
	section := func(label, meta string, keys []helpKey) {
		if tall {
			lines = append(lines, LogLine{Kind: "gap"})
		}
		lines = append(lines, h2Line(label, meta, width))
		for _, k := range keys {
			if k.name == "" && k.key == nil {
				for _, w := range wrap(k.what, max(8, width-2)) {
					lines = append(lines, LogLine{Kind: "muted", Text: "  " + w})
				}
				continue
			}
			key := k.key
			if key == nil {
				key = []Span{{Text: k.name, Role: "text", Bold: true}}
			}
			name := spanText(key)
			if width < 34 || cellLen(name) >= helpKeyCol {
				lines = append(lines, spanLine("text", append([]Span{{Text: "  "}}, key...)...))
				for _, w := range wrap(k.what, max(8, width-4)) {
					lines = append(lines, LogLine{Kind: "muted", Text: "    " + w})
				}
				continue
			}
			for i, w := range wrap(k.what, width-2-helpKeyCol) {
				if i == 0 {
					spans := append(append([]Span{{Text: "  "}}, key...), Span{Text: padRight("", helpKeyCol-cellLen(name)) + w, Role: "muted"})
					lines = append(lines, spanLine("text", spans...))
					continue
				}
				lines = append(lines, LogLine{Kind: "muted", Text: padRight("", 2+helpKeyCol) + w})
			}
		}
	}
	keys := pageKeys[page]
	if page == "task" {
		keys = pageKeys["about"]
	}
	section("THIS PAGE", page, keys)
	section("EVERYWHERE", "", []helpKey{
		{name: "tab", what: "switch plan / agent"},
		{name: "?", what: "this help"},
		{name: "ctrl+l", what: "repaint"},
		{name: "ctrl+c ×2", what: "quit"},
	})
	section("MARKS", "", []helpKey{
		{what: "pinned: the active task, or next up", key: []Span{{Text: "▸", Role: "title", Bold: true}}},
		{name: "○ · ✓", what: "ready · waiting on another task · done"},
		{name: "– !", what: "skipped · blocked"},
		{name: "1/3", what: "a parent's progress; on row 1, the plan's"},
		{what: "your cursor", key: []Span{{Text: "▌", Role: "pick"}}},
		{what: "news on the other tab (! an ask or an error)", key: []Span{{Text: "•", Role: "accent", Bold: true}, {Text: " "}, {Text: "!", Role: "error", Bold: true}}},
		{name: "following", what: "Rex opens each plan your agent writes"},
		{name: "latched", what: "you picked this plan; another the agent writes shows on row 1, and enter switches"},
	})
	section("AGENT TAB", "", []helpKey{
		{name: "thinking", what: "a run of N thoughts (×N); the latest shows"},
		{name: "$", what: "a command; over 5 in a row, all but the last 4 fold into one line"},
		{name: "✗", what: "a failed step, or the turn's own error"},
		{what: "the agent waits on you", key: pill("ASK", "pickbg", "error")},
		{name: "reply ───", what: "the agent's reply"},
		{name: "rex ›", what: "Rex's own line, plan news or drift, never sent"},
	})
	section("PLAN LIST", "", []helpKey{
		{name: "type", what: "find a plan, filter a folder, or name a new one"},
		{name: "a/b", what: "plan b in folder a"},
		{name: "ctrl+u", what: "clear the line"},
		{name: "ctrl+w", what: "delete a word"},
		{name: "? /help", what: "this help (? on an empty line)"},
		{name: "/breakdance", what: "one dance"},
	})
	section("REX NEVER", "", nil)
	for _, w := range wrap("runs or approves tools, edits a plan (it only creates an empty one you name in the plan list), sends anything to your agent, or reads transcripts. Your agent pins and finishes tasks in the plan file; Rex shows it.", max(8, width-2)) {
		lines = append(lines, LogLine{Kind: "text", Text: "  " + w})
	}
	return lines
}
