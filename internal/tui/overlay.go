package tui

import "slices"

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

const (
	// helpKeyCol is the key column of Help's rows and the feed's: the key
	// and its padding, after a 4-cell indent.
	helpKeyCol = 12
	// helpStackBelow is the text width under which a key sits above what it
	// does, at the indent, and what it does under it.
	helpStackBelow = 36
)

// helpKey is one row of Help: a key (or a mark) and what it does. key
// overrides the key's look, for marks that keep their own colors. With
// neither a name nor a key it is a note under the keys, with no key column.
type helpKey struct {
	name, what string
	key        []Span
}

// pageKeys is This page in Help: every key the page it was opened over
// takes, not only its footer's (docs/ux.md §13). o, which opens a plan
// the notice row offers, reads the same on every page that takes it.
var pageKeys = map[string][]helpKey{
	"tasks": {
		{name: "↑↓", what: "select a task · pgup pgdn a page"},
		{name: "home end", what: "first / last task (also ctrl+a ctrl+e)"},
		{name: "enter →", what: "open its details"},
		pinHelp,
		doneHelp,
		{name: "i", what: "about this plan: its goal, scope and progress"},
		offerHelp,
		{name: "esc ←", what: "back to Plans"},
	},
	// A plan with no list: no task to move to or open, only its page to
	// scroll on a short screen (docs/ux.md §6).
	"no tasks": {
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		{name: "i", what: "about this plan: its goal, scope and progress"},
		offerHelp,
		{name: "esc ←", what: "back to Plans"},
	},
	"can't read": {
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		offerHelp,
		{name: "esc ←", what: "back to Plans"},
	},
	"task": {
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		pinHelp,
		doneHelp,
		offerHelp,
		{name: "esc ←", what: "back to the task list, on this task"},
	},
	"about": {
		{name: "↑↓", what: "scroll · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		offerHelp,
		{name: "esc ←", what: "back to the task list"},
	},
	"plans": slices.Concat([]helpKey{
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "enter →", what: "the selected card or plan"},
		{name: "type", what: "find a plan by name or title across rex/plans"},
	}, typingKeys, []helpKey{
		offerHelp,
		{what: "Continue and Open latest follow the plans your agent writes; a plan picked anywhere else stays until you pick another"},
	}),
	"open a plan": slices.Concat([]helpKey{
		{name: "↑↓", what: "move · pgup pgdn a page"},
		{name: "home end", what: "first / last (also ctrl+a ctrl+e)"},
		{name: "enter →", what: "open the plan, or go into the folder"},
		{name: "type", what: "filter this folder"},
	}, typingKeys, []helpKey{
		{name: "esc ←", what: "up a folder; from rex/plans/, back to Plans"},
	}),
	"new plan": slices.Concat([]helpKey{
		{name: "enter", what: "create it and open its task list"},
		{name: "type", what: "its name"},
		{name: "a/b", what: "plan b in folder a"},
	}, typingKeys, []helpKey{
		{name: "esc", what: "cancel: nothing is written"},
	}),
	// The empty AGENT tab, feed health: a page (docs/ux.md §10).
	"no events": {
		{name: "↑↓", what: "scroll this page on a short screen · pgup pgdn a page"},
		{name: "home end", what: "top / bottom (also ctrl+a ctrl+e)"},
		offerHelp,
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

// typingKeys are what Plans' text field takes, on each Plans page
// (docs/ux.md §2): a /word is a verb.
var typingKeys = []helpKey{
	{name: "ctrl+u", what: "clear the field"},
	{name: "ctrl+w", what: "delete a word"},
	{name: "/help", what: "this help, from the field (? with it empty)"},
	{name: "/breakdance", what: "one dance"},
}

// offerHelp is o in This page, on every page where it opens an offer.
var offerHelp = helpKey{name: "o", what: "open the plan your agent wrote, while the notice row offers it"}

// pinHelp and doneHelp are p and d in This page, on the task list and a
// task's details (docs/ux.md §7b): each writes the task's status into the
// plan file, and the same key undoes it.
var (
	pinHelp  = helpKey{name: "p", what: "pin this task, or unpin the pin; a wired agent gets the pin with each prompt, and row 1 says whether it went"}
	doneHelp = helpKey{name: "d", what: "mark this task done, or reopen a done one"}
)

// planMarks and agentMarks are Marks in Help (docs/ux.md §13): the marks
// of the place Help was opened in.
var (
	planMarks = []helpKey{
		{what: "where you are; enter acts here", key: []Span{{Text: "❯", Role: "pick"}}},
		{name: doneMark + " – !", what: "done · skipped · blocked; no mark: still to do"},
		{what: "the task you (p) or your agent set active: a wired agent gets it with each prompt, and row 1 says whether it went", key: []Span{{Text: "pinned", Role: "accent"}}},
		{what: "nothing pinned: the task Rex would pin, never sent", key: []Span{{Text: "next up", Role: "muted"}}},
		{what: "a second active task: a problem in the plan", key: []Span{{Text: "also active", Role: "error"}}},
		{what: "a task in a loop: it waits on itself, or is its own parent", key: []Span{{Text: "cycle", Role: "error"}}},
		{name: "1/3", what: "a group's progress; on row 0, the plan's"},
		{name: "following", what: "Rex opens each plan your agent writes"},
		{name: "latched", what: "you picked this plan; another the agent writes shows on the notice row and in Open latest, and o opens it"},
	}
	agentMarks = []helpKey{
		{name: promptMark, what: "your prompt"},
		{name: "with pin", what: "under a prompt: the pin it carried to your agent (with plan: a new plan's name)"},
		{name: "thinking", what: "a run of N thoughts (×N); the latest shows"},
		{name: "$", what: "a shell command your agent ran"},
		{name: "edit", what: "a file your agent changed (wrote, patched or deleted); other tools read as they report them"},
		{name: "… +N", what: "steps folded: over 5 in a row, all but the last 4"},
		{name: failMark, what: "a failed step, or the turn's own error"},
		{name: askMark, what: "your agent asks you something"},
		{what: "the edit or prompt on this row was off the pin; seen from PLAN, its notice row says why", key: []Span{{Text: hardTag, Role: "accent"}}},
		{what: "near the pin but not on it, such as a file next to one it lists; only the tag says so", key: []Span{{Text: softTag, Role: "muted"}}},
		{name: "rex ·", what: "Rex's own line, never sent"},
	}
)

// helpLines is Help (docs/ux.md §13): This page, for the page it opened
// over, then what holds everywhere, the place's marks and the pin's send
// state; the feed's health follows (app.helpBody). Each section is a bold
// label; tall puts a blank row above each but the first. From
// helpStackBelow cells a key sits in a 12-cell column left of what it
// does; narrower it sits above it.
func helpLines(width int, page string, tall bool) []LogLine {
	var lines []LogLine
	section := func(label string, keys []helpKey) {
		if tall && len(lines) > 0 { // the first section's is row 2, under the title
			lines = append(lines, LogLine{Kind: "gap"})
		}
		lines = append(lines, labelLine(label))
		for _, k := range keys {
			lines = append(lines, helpRows(k, width)...)
		}
	}
	section("This page", pageKeys[page])
	section("Everywhere", []helpKey{
		{name: "tab", what: "switch between Plan and Agent"},
		{name: "?", what: "this help · esc closes it"},
		{name: "ctrl+l", what: "repaint"},
		{name: "ctrl+c ×2", what: "quit"},
	})
	marks := planMarks
	if page == "agent" || page == "no events" {
		marks = agentMarks
	}
	section("Marks", marks)
	// The pin line's right side (docs/ux.md §4, §7a): where `? help` sends
	// you, Feed below says which command fixes it.
	section("Your agent and the pin", []helpKey{
		{name: "sent", what: "your last prompt carried the pin to your agent"},
		{name: "sends", what: "not sent yet: it goes with your next prompt"},
		{name: "not sent", what: "nothing is pinned, or this plan sends nothing (read-only, or not the plan Rex last opened here); with ? help, no wired agent takes it, or your agent's build doesn't read it, and Feed below says what to run"},
	})
	return lines
}

// helpRows is one key of Help: the key in text at column 4, what it does
// muted in the column after it (docs/ux.md §13). A note, with no key, is
// muted at column 4.
func helpRows(k helpKey, width int) []LogLine {
	const indent = "    "
	if k.name == "" && k.key == nil {
		var lines []LogLine
		for _, w := range wrap(k.what, max(8, width-4)) {
			lines = append(lines, LogLine{Kind: "muted", Text: indent + w})
		}
		return lines
	}
	key := k.key
	if key == nil {
		key = []Span{{Text: k.name, Role: "text"}}
	}
	name := spanText(key)
	var lines []LogLine
	if width < helpStackBelow || cellLen(name) >= helpKeyCol {
		lines = append(lines, spanLine("text", append([]Span{{Text: indent}}, key...)...))
		for _, w := range wrap(k.what, max(8, width-6)) {
			lines = append(lines, LogLine{Kind: "muted", Text: indent + "  " + w})
		}
		return lines
	}
	for i, w := range wrap(k.what, width-len(indent)-helpKeyCol) {
		if i == 0 {
			spans := append(append([]Span{{Text: indent}}, key...), Span{Text: padRight("", helpKeyCol-cellLen(name)) + w, Role: "muted"})
			lines = append(lines, spanLine("text", spans...))
			continue
		}
		lines = append(lines, LogLine{Kind: "muted", Text: padRight("", len(indent)+helpKeyCol) + w})
	}
	return lines
}
