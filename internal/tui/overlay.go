package tui

// overlayKind is what covers a place's body: only Help now. p and d act at
// once, with no confirm: the same key undoes (docs/ux.md §1, §7b).
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
// takes, not only its footer's (docs/ux.md §13). o, which opens a plan
// the notice row offers, reads the same on every page that takes it.
var pageKeys = map[string][]helpKey{
	"tasks": {
		{name: "enter →", what: "the selected task's details"},
		pinHelp,
		doneHelp,
		{name: "i", what: "about this plan: its goal, scope and progress"},
		offerHelp,
		{name: "↑↓", what: "move between tasks · pgup pgdn a page"},
		{name: "home end", what: "first / last task (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to Plans"},
	},
	// A plan with no list: no task to move to or open, only its page to
	// scroll on a short screen (docs/ux.md §6).
	"no tasks": {
		{name: "i", what: "about this plan: its goal, scope and progress"},
		offerHelp,
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to Plans"},
	},
	"can't read": {
		offerHelp,
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to Plans"},
	},
	"task": {
		pinHelp,
		doneHelp,
		offerHelp,
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to the task list, on this task"},
	},
	"about": {
		offerHelp,
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "esc ←", what: "back to the task list"},
	},
	"plans": {
		{name: "enter →", what: "the selected card or plan"},
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "type", what: "find a plan by name or title across rex/plans"},
		offerHelp,
		{what: "Continue and Open latest follow the plans your agent writes; a plan picked anywhere else stays until you pick another"},
	},
	"open a plan": {
		{name: "enter →", what: "open the plan, or go into the folder"},
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "type", what: "filter this folder"},
		{name: "esc ←", what: "up a folder; from rex/plans/, back to Plans"},
	},
	"new plan": {
		{name: "enter", what: "create it and open its task list"},
		{name: "type", what: "its name; a/b makes plan b in folder a"},
		{name: "esc", what: "cancel: nothing is written"},
	},
	"agent": {
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home", what: "oldest (also ctrl+a)"},
		{name: "end", what: "live (also ctrl+e)"},
		offerHelp,
		// No key unfolds a run (docs/ux.md §10), so this is a note, not a key.
		{what: "folding is display only: the feed keeps every thought"},
	},
}

// offerHelp is o in THIS PAGE, on every page where it opens an offer.
var offerHelp = helpKey{name: "o", what: "open the plan your agent wrote, while the notice row offers it"}

// pinHelp and doneHelp are p and d in THIS PAGE, on the task list and a
// task's details (docs/ux.md §7b): each writes the task's status into the
// plan file, and the same key undoes it.
var (
	pinHelp  = helpKey{name: "p", what: "pin this task, or unpin the pin; your agent gets the pin with every prompt"}
	doneHelp = helpKey{name: "d", what: "mark this task done, or reopen a done one"}
)

// helpLines is Help (docs/ux.md §13): THIS PAGE, for the page it opened over,
// then what holds everywhere, the marks, how AGENT names its blocks,
// typing on Plans and what Rex never does; the feed's health follows.
// tall puts a blank row above each section but the first. From 34 cells the key sits in
// an 11-cell column left of what it does; narrower it sits above it.
func helpLines(width int, page string, tall bool) []LogLine {
	var lines []LogLine
	section := func(label, meta string, keys []helpKey) {
		if tall && len(lines) > 0 { // the first section's is row 2, under the title
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
	section("THIS PAGE", page, pageKeys[page])
	section("EVERYWHERE", "", []helpKey{
		{name: "tab", what: "switch plan / agent"},
		{name: "?", what: "this help"},
		{name: "ctrl+l", what: "repaint"},
		{name: "ctrl+c ×2", what: "quit"},
	})
	section("MARKS", "", []helpKey{
		{name: doneMark + " – !", what: "done · skipped · blocked; no mark: still to do"},
		{what: "the task you (p) or your agent set active, which your agent gets with every prompt", key: []Span{{Text: "pinned", Role: "accent"}}},
		{what: "nothing pinned: the task Rex would pin, never sent", key: []Span{{Text: "next up", Role: "muted"}}},
		{what: "a second active task: a problem in the plan", key: []Span{{Text: "also active", Role: "error"}}},
		{what: "a task in a loop: it waits on itself, or is its own parent", key: []Span{{Text: "cycle", Role: "error"}}},
		{name: "1/3", what: "a group's progress; on row 0, the plan's"},
		{what: "your cursor", key: []Span{{Text: "❯", Role: "pick"}}},
		{name: "following", what: "Rex opens each plan your agent writes"},
		{name: "latched", what: "you picked this plan; another the agent writes shows on the notice row and in Open latest, and o opens it"},
	})
	section("AGENT TAB", "", []helpKey{
		{name: "thinking", what: "a run of N thoughts (×N); the latest shows"},
		{name: "$", what: "a command; over 5 in a row, all but the last 4 fold into one line"},
		{name: "✗", what: "a failed step, or the turn's own error"},
		{what: "the agent waits on you", key: pill("ASK", "text")},
		{name: "reply ───", what: "the agent's reply"},
		{name: "rex ›", what: "Rex's own line, plan news or drift, never sent"},
	})
	section("PLANS", "", []helpKey{
		{name: "type", what: "find a plan, filter a folder, or name a new one"},
		{name: "a/b", what: "plan b in folder a"},
		{name: "ctrl+u", what: "clear the line"},
		{name: "ctrl+w", what: "delete a word"},
		{name: "? /help", what: "this help (? with the field empty)"},
		{name: "/breakdance", what: "one dance"},
	})
	section("REX NEVER", "", nil)
	for _, w := range wrap("runs or approves tools, writes a plan beyond the status values you set with p and d and an empty one you name on Plans, sends your agent anything but the pinned task before each prompt (or a new plan that has no tasks yet), or reads transcripts. You and your agent both pin and finish tasks in the plan file; Rex shows who did.", max(8, width-2)) {
		lines = append(lines, LogLine{Kind: "text", Text: "  " + w})
	}
	return lines
}
