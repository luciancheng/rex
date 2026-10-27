package tui

import (
	"fmt"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
)

// TextOnlyBelow is the width under which the mascot is hidden so the text
// column keeps the whole terminal (docs/ux.md §12).
const TextOnlyBelow = 40 + Gutter + MinTextCols

// layout is how a terminal size splits into mascot, text and composer.
type layout struct {
	mascotCols int // 0 in text-only mode
	gutter     int
	textW      int // 0 when the text column would be too narrow
	page       int // 1 when row 2 is the page title (docs/ux.md §4)
	composer   int // 3 = boxed, 1 = one bare row, 0 = none
	bodyH      int
	locked     bool // too narrow: only Ctrl+C (and Ctrl+L) do anything
	typing     bool // tall enough for a composer: the plan list takes typing
}

func layoutFor(columns, lines int) layout {
	columns, lines = max(1, columns), max(1, lines)
	var l layout
	if columns >= TextOnlyBelow {
		l.mascotCols = min(mascot.StageSize, columns)
		l.gutter = Gutter
		l.textW = columns - l.mascotCols - l.gutter
	} else {
		l.textW = columns
	}
	if l.textW < MinTextCols {
		l.textW, l.mascotCols, l.gutter, l.locked = 0, 0, 0, true
	}
	switch {
	case lines >= 16:
		l.composer = InputRows
	case lines >= 7:
		l.composer = 1
	}
	if lines > 3 {
		l.page = 1 // never on the footer's row
	}
	l.typing = l.composer > 0 && !l.locked
	l.bodyH = max(0, lines-2-l.page-FooterRows-l.composer)
	return l
}

// bare is l on a screen without a composer: the body takes its rows.
// typing still says whether the terminal is tall enough to type in.
func (l layout) bare() layout {
	l.bodyH += l.composer
	l.composer = 0
	return l
}

// untitled is l on a screen without a page title: AGENT under
// agentTitleRows rows, where the body takes row 2 too.
func (l layout) untitled() layout {
	l.bodyH += l.page
	l.page = 0
	return l
}

// agentTitleRows is the height from which AGENT has a page title; PLAN
// screens and Help always do (docs/ux.md §4).
const agentTitleRows = 12

// brandFrom is the text width from which row 0 starts with REX; narrower,
// the mascot (or nothing) is the brand.
const brandFrom = 42

// tabWords are the two places, in the order row 0 always shows them.
var tabWords = [2]string{PaneChat: "AGENT", PanePlans: "PLAN"}

// tabBlock is the cells the tabs and their cue take: "[PLAN]•AGENT" and
// "PLAN•[AGENT]" alike.
const tabBlock = 12

// headerRow is row 0, the tab bar (docs/ux.md §4): REX from brandFrom
// cells; both places, the one on screen a pill, with the cue cell between
// them (a 12-cell block, so the turn word never moves); the turn word;
// and on the right the follow word ("following" or "latched") and the
// open plan's path, shortened to fit. The word stays while it fits beside
// min(5, stem) cells of path, judged against the longest turn word and
// the longer follow word, so it doesn't come and go with the turn; a
// dropped word never gives ".rex.json" its room back.
func headerRow(width int, pane PaneKind, cue, state, follow, planRel string) LogLine {
	state = strings.TrimSpace(flat(state))
	if state == "" {
		state = "idle"
	}
	var spans []Span
	brand := 0
	if width >= brandFrom {
		spans = append(spans, Span{Text: "REX", Role: "title", Bold: true}, Span{Text: " "})
		brand = cellLen("REX ")
	}
	// The cue's glyph says which; neither is yellow, which is only the
	// pin's (docs/ux.md §5).
	mark := Span{Text: " "}
	if cue == "•" || cue == "!" {
		mark = Span{Text: cue, Role: "text", Bold: true}
	}
	if pane == PanePlans {
		spans = append(append(spans, pill(tabWords[PanePlans], "text")...), mark, Span{Text: tabWords[PaneChat], Role: "muted"})
	} else {
		spans = append(append(spans, Span{Text: tabWords[PanePlans], Role: "muted"}, mark), pill(tabWords[PaneChat], "text")...)
	}
	turn := Span{Text: state, Role: "muted"}
	switch state {
	case "thinking", "running":
		turn.Role = "text"
	case "ask":
		turn.Role, turn.Bold = "text", true
	case "error":
		turn.Role, turn.Bold = "error", true
	}
	spans = append(spans, Span{Text: " "}, turn)
	left := cellLen(spanText(spans))
	var right []Span
	if planRel = strings.TrimSpace(flat(planRel)); planRel != "" {
		stem := strings.TrimSuffix(strings.TrimPrefix(planRel, "rex/plans/"), ".rex.json")
		room := width - left - 2
		keep := width - brand - tabBlock - 1 - max(cellLen(state), cellLen("thinking")) - 2 - cellLen("following ")
		switch {
		case follow == "":
			if path := shortPlanPath(planRel, room); path != "" {
				right = append(right, Span{Text: path, Role: "muted"})
			}
		case keep >= min(headPathMin, cellLen(stem)):
			role := "muted"
			if follow == "latched" {
				role = "text"
			}
			right = append(right, Span{Text: follow, Role: role})
			if path := shortPlanPath(planRel, room-cellLen(follow)-1); path != "" {
				right = append(right, Span{Text: " " + path, Role: "muted"})
			}
		default:
			if path := shortPlanPath(stem, room); path != "" {
				right = append(right, Span{Text: path, Role: "muted"})
			}
		}
	}
	if len(right) > 0 {
		spans = append(spans, Span{Text: strings.Repeat(" ", max(2, width-left-cellLen(spanText(right))))})
		spans = append(spans, right...)
	}
	return spanLine("muted", spans...)
}

// headPathMin is the fewest cells of the plan's path row 0 keeps: "…demo".
const headPathMin = 5

// shortPlanPath fits a repo-relative plan path into room cells: the whole
// path when it fits, then without "rex/plans/", then without ".rex.json",
// then "…/" and its last segment, then "…" and its tail, down to
// headPathMin cells; under that it is dropped.
func shortPlanPath(path string, room int) string {
	stem := strings.TrimSuffix(strings.TrimPrefix(path, "rex/plans/"), ".rex.json")
	for _, p := range []string{path, strings.TrimPrefix(path, "rex/plans/"), stem} {
		if cellLen(p) <= room {
			return p
		}
	}
	if i := strings.LastIndex(stem, "/"); i >= 0 {
		if p := "…/" + stem[i+1:]; cellLen(p) <= room {
			return p
		}
	}
	if room < headPathMin {
		return ""
	}
	return "…" + tailCells(stem, room-1)
}

// withoutEnter is the plan list's footer when the screen is too short to
// show its composer (docs/ux.md §12). Typing is ignored there, so "type
// to …" goes; enter never submits a draft blind, so every enter goes but
// "enter open" on an empty draft, which opens the selected row; and with
// a hidden draft ? would be text, so "? help" goes too. Screens without a
// composer keep their footers, since their keys act at once.
func withoutEnter(segs []FooterSeg, draft string) []FooterSeg {
	var out []FooterSeg
	for _, s := range segs {
		switch {
		case strings.HasPrefix(s.Text, "type to "):
		case strings.HasPrefix(s.Text, "enter ") && (s.Text != "enter open" || draft != ""):
		case s == helpSeg && draft != "":
		default:
			out = append(out, s)
		}
	}
	return out
}

// footerSegments is the footer for the current screen, highest rank first
// (docs/ux.md §4): keys only, navigation first.
func footerSegments(opt ComposeOptions, scroll int) ([]FooterSeg, string) {
	if opt.QuitPending {
		return segs("ctrl+c again to quit"), "error"
	}
	if opt.Footer != nil {
		return opt.Footer, "muted"
	}
	if opt.Pane == PanePlans && opt.Plans != nil {
		out := listFooter(opt.Plans, opt.Draft, opt.Preview)
		if n := runeLen(opt.Draft); n >= draftCap-16 && len(out) > 0 && opt.Composer {
			// The cap is not silent: near it, the count is a must-have.
			out = append([]FooterSeg{out[0], {Text: fmt.Sprintf("%d/%d", n, draftCap)}}, out[1:]...)
		}
		return out, "muted"
	}
	return agentFooter(scroll, false, false), "muted"
}

// hintText is how a placeholder reads: dim when there is color, in
// parentheses when there isn't, so it never looks like typed text.
func hintText(placeholder string, enabled bool) string {
	if enabled || placeholder == "" {
		return placeholder
	}
	return "(" + placeholder + ")"
}

// listFooter is the plan list's footer (docs/ux.md §4): what enter does
// on the selected row, where esc goes, tab, then ? help while ? opens
// Help (on an empty draft), then the rest.
func listFooter(b *PlanBrowser, draft, preview string) []FooterSeg {
	if preview != "" {
		return segs(preview, "esc clear", "tab agent")
	}
	enter := "enter open"
	if b.Cursor < len(b.Rows) {
		switch b.Rows[b.Cursor].Kind {
		case PlanRowNew, PlanRowMkdir, PlanRowCreateNamed:
			enter = "enter create" // short enough that ? help fits at 38 cells in Browse too
		}
	}
	switch {
	case b.Prompting() && draft == "":
		return []FooterSeg{{Text: "enter create"}, {Text: "esc cancel"}, {Text: "tab agent"}, helpSeg}
	case b.Prompting():
		return segs("enter create", "esc clear", "tab agent")
	case draft != "":
		return segs(enter, "esc clear", "tab agent", "↑↓ move")
	case b.Browsing():
		esc := "esc up"
		if b.topFolder() {
			esc = "esc plans" // from a top folder Esc is the plan list (docs/ux.md §11)
		}
		return []FooterSeg{{Text: enter}, {Text: esc}, {Text: "tab agent"}, helpSeg, {Text: "↑↓ move"}, {Text: "type to filter"}}
	case b.Current == "":
		// With no plan open, Esc goes back to the Wait screen (docs/ux.md
		// §3); "esc wait" keeps ? at 38 cells beside "enter create".
		return []FooterSeg{{Text: enter}, {Text: "esc wait"}, {Text: "tab agent"}, helpSeg, {Text: "↑↓ move"}, {Text: "type to find"}}
	}
	return []FooterSeg{{Text: enter}, {Text: "tab agent"}, helpSeg, {Text: "↑↓ move"}, {Text: "type to find"}}
}

// agentFooter is AGENT's footer (docs/ux.md §4). Scrolled, end live leads,
// and under agentTitleRows the scroll count leads it, since there is no
// page title to carry it; offer is row 1's new plan that enter takes, and
// it takes end live's place, since ↑↓ still reach live. ? help ranks over
// ↑↓ scroll, so it is on every AGENT footer at 38 cells.
func agentFooter(scroll int, titled, offer bool) []FooterSeg {
	var out []FooterSeg
	if scroll > 0 && !titled {
		out = append(out, FooterSeg{Text: fmt.Sprintf("%d↑", scroll)})
	}
	switch {
	case offer:
		out = append(out, FooterSeg{Text: "enter switch"})
	case scroll > 0:
		out = append(out, FooterSeg{Text: "end live"})
	}
	out = append(out, FooterSeg{Text: "tab plan"}, helpSeg, FooterSeg{Text: "↑↓ scroll"})
	if scroll == 0 && !offer {
		out = append(out, segs("home oldest", "ctrl+c ×2 quit")...)
	}
	return out
}

// emptyFooter is the empty AGENT tab's footer (docs/ux.md §4): a page, so,
// as on Wait, ↑↓ scroll only while it runs past the screen, and there is
// no oldest or live to go to.
func emptyFooter(more, offer bool) []FooterSeg {
	var out []FooterSeg
	if offer {
		out = append(out, FooterSeg{Text: "enter switch"})
	}
	out = append(out, FooterSeg{Text: "tab plan"}, helpSeg)
	if more {
		out = append(out, FooterSeg{Text: "↑↓ scroll"})
	}
	if !offer {
		out = append(out, FooterSeg{Text: "ctrl+c ×2 quit"})
	}
	return out
}

// composerRows draws the composer: a box when there is room, a bare row on
// short terminals.
func composerRows(l layout, draft, prefix, placeholder string, enabled bool) []string {
	switch l.composer {
	case InputRows:
		return inputBox(draft, l.textW, enabled, prefix, placeholder)
	case 1:
		if draft == "" && placeholder != "" {
			return []string{mascot.Paint(prefix, mascot.UI["text"], enabled) + mascot.Paint(clip(hintText(placeholder, enabled), max(0, l.textW-cellLen(prefix))), mascot.UI["border"], enabled)}
		}
		line := clip(prefix+visibleDraft(draft, l.textW-cellLen(prefix)), l.textW)
		return []string{mascot.Paint(line, mascot.UI["text"], enabled)}
	}
	return nil
}

// cursorFor places the cursor after the draft, or hides it (row 0).
func cursorFor(l layout, lines int, draft, prefix string) (int, int) {
	visible := visibleDraft(draft, l.textW-l.composerChrome()-cellLen(prefix))
	switch l.composer {
	case InputRows:
		return lines - 2, l.mascotCols + l.gutter + 1 + 1 + cellLen(prefix) + cellLen(visible)
	case 1:
		return lines - 1, l.mascotCols + l.gutter + 1 + cellLen(prefix) + cellLen(visible)
	}
	return 0, 0
}

func (l layout) composerChrome() int {
	if l.composer == InputRows {
		return 2
	}
	return 0
}
