package tui

import (
	"fmt"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
)

// TextOnlyBelow and textOnlyRows are the terminal size under which the
// mascot is hidden and the text column takes the whole width, up to
// maxText (docs/ux.md §12). At 110 columns the mascot, the gutter and a
// 68-cell text column fit; at 80 the mascot would leave the plan 38.
const (
	TextOnlyBelow = 110
	textOnlyRows  = 16
)

// maxText is the most of a wide terminal the text column takes (docs/ux.md
// §4): wider, the rest of the row stays empty.
const maxText = 100

// blankFrom is the height from which row 2 is blank and the notice row
// is H−2 (docs/ux.md §4); AGENT puts blank rows between its blocks from
// there too (§10). Shorter, a notice takes row 1 while it shows.
const blankFrom = 12

// layout is how a terminal size splits into mascot and text.
type layout struct {
	mascotCols int // 0 in text-only mode
	gutter     int
	textW      int // 0 when the text column would be too narrow
	blank      int // 1 when row 2 is the blank row (docs/ux.md §4)
	notice     int // 1 when H−2 is the notice row
	bodyH      int
	locked     bool // too narrow: only Ctrl+C (and Ctrl+L) do anything
	typing     bool // tall enough to type: Plans' text field takes keys (docs/ux.md §12)
	boxed      bool // tall enough for the text field's box (docs/ux.md §2)
}

func layoutFor(columns, lines int) layout {
	columns, lines = max(1, columns), max(1, lines)
	var l layout
	if columns >= TextOnlyBelow && lines >= textOnlyRows {
		l.mascotCols = mascot.StageSize
		l.gutter = Gutter
		l.textW = columns - l.mascotCols - l.gutter
	} else {
		l.textW = columns
	}
	if l.textW < MinTextCols {
		l.textW, l.locked = 0, true
	}
	l.textW = min(l.textW, maxText)
	if lines >= blankFrom {
		l.blank, l.notice = 1, 1
	}
	l.typing = lines >= typeFrom && !l.locked
	l.boxed = lines >= boxFrom
	l.bodyH = max(0, lines-2-l.blank-l.notice-FooterRows)
	return l
}

// typeFrom is the height from which Plans' text field takes keys, and
// boxFrom the one from which it is a box rather than one row (docs/ux.md
// §2, §12).
const (
	typeFrom = 7
	boxFrom  = 16
)

// labelFrom is the text width from which row 0 ends with the places'
// label, "plan · agent" (docs/ux.md §4).
const labelFrom = 30

// placeWords are the two places as row 0's label names them, in its order.
var placeWords = [2]string{PanePlans: "plan", PaneChat: "agent"}

// placeLabel is row 0's quiet label (docs/ux.md §4): both places, the one
// on screen bright text and the other and the dot muted; short, the place
// on screen alone. It never blinks or changes color with news.
func placeLabel(pane PaneKind, short bool) []Span {
	here := Span{Text: placeWords[pane], Role: "text"}
	if short {
		return []Span{here}
	}
	other := PaneChat
	if pane == PaneChat {
		other = PanePlans
	}
	there := Span{Text: placeWords[other], Role: "muted"}
	if pane == PanePlans {
		return []Span{here, {Text: " · ", Role: "muted"}, there}
	}
	return []Span{there, {Text: " · ", Role: "muted"}, here}
}

// otherPlace is the footer's word for Tab: the place not on screen.
func otherPlace(pane PaneKind) string {
	if pane == PanePlans {
		return placeWords[PaneChat]
	}
	return placeWords[PanePlans]
}

// withoutEnter is Plans' footer when the screen is too short to type in
// (docs/ux.md §12): the text field is hidden and typing is ignored, so
// "type to …" goes; enter never submits a draft blind, so enter goes
// while the draft has text, and in New plan, which nothing can be typed
// into; with a hidden draft ? would be text, so "? help" goes too. None of
// it depends on the cursor. Screens without a text field keep their
// footers, since their keys act at once.
func withoutEnter(segs []FooterSeg, draft string, prompting bool) []FooterSeg {
	var out []FooterSeg
	for _, s := range segs {
		switch {
		case strings.HasPrefix(s.Text, "type to "):
		case strings.HasPrefix(s.Text, "enter ") && (draft != "" || prompting):
		case s == helpSeg && draft != "":
		default:
			out = append(out, s)
		}
	}
	return out
}

// footerSegments is the footer for the current screen, in display order
// (docs/ux.md §4): keys only, ? help last.
func footerSegments(opt ComposeOptions, scroll int) []FooterSeg {
	if opt.QuitPending {
		return segs("ctrl+c again to quit")
	}
	if opt.Footer != nil {
		return opt.Footer
	}
	if opt.Pane == PanePlans && opt.Plans != nil {
		out := listFooter(opt.Plans, opt.Draft, opt.Preview)
		if n := runeLen(opt.Draft); n >= draftCap-16 && len(out) > 0 && opt.Composer {
			// The cap is not silent: near it, the count is a must-have, after
			// what enter does.
			at := 0
			for at < len(out) && !strings.HasPrefix(out[at].Text, "enter ") {
				at++
			}
			at = min(at+1, len(out))
			count := FooterSeg{Text: fmt.Sprintf("%d/%d", n, draftCap), Rank: 1}
			out = append(append(append([]FooterSeg(nil), out[:at]...), count), out[at:]...)
		}
		return out
	}
	return agentFooter()
}

// enterOn is what enter does on Plans' selected row, as the footer says
// it (docs/ux.md §4): opens a plan or a folder, creates one, or browses
// rex/plans. Its width is the longest, so the cursor never changes the
// footer.
func enterOn(b *PlanBrowser) FooterSeg {
	s := seg("enter open", "enter create", "enter browse")
	if b.Cursor < len(b.Rows) {
		switch b.Rows[b.Cursor].Kind {
		case PlanRowNew, PlanRowCreateNamed:
			s.Text = "enter create"
		case PlanRowOpen:
			s.Text = "enter browse"
		}
	}
	return s
}

// listFooter is Plans' footer (docs/ux.md §4): ↑↓ while there is more than
// one row, what enter does on the selected row, where esc goes, typing,
// tab, then ? help while ? opens Help (with the field empty).
func listFooter(b *PlanBrowser, draft, preview string) []FooterSeg {
	tab := FooterSeg{Text: "tab agent"}
	if preview != "" {
		return []FooterSeg{{Text: preview}, {Text: "esc clear"}, tab}
	}
	if b.Prompting() {
		out := []FooterSeg{{Text: "enter create"}, {Text: "esc cancel"}, tab}
		if draft == "" {
			out = append(out, helpSeg)
		}
		return out
	}
	var out []FooterSeg
	if len(b.Rows) > 1 {
		out = append(out, FooterSeg{Text: "↑↓ select"})
	}
	if len(b.Rows) > 0 {
		out = append(out, enterOn(b))
	}
	switch {
	case draft != "":
		return append(out, FooterSeg{Text: "esc clear"}, tab)
	case b.Browsing():
		esc := seg("esc up", "esc plans")
		if b.topFolder() {
			esc.Text = "esc plans" // from rex/plans itself Esc is Plans (docs/ux.md §11)
		}
		return append(out, esc, FooterSeg{Text: "type to filter"}, tab, helpSeg)
	case b.total > 0:
		out = append(out, FooterSeg{Text: "type to find"})
	}
	return append(out, tab, helpSeg)
}

// agentFooter is AGENT's footer (docs/ux.md §4): scrolling, its ends,
// Tab and Help. A new plan's offer is taken with o, which its notice
// names, never the footer.
func agentFooter() []FooterSeg {
	return []FooterSeg{{Text: "↑↓ scroll"}, {Text: "home oldest"}, {Text: "end live"}, {Text: "tab plan"}, helpSeg}
}

// pageFooter is the footer of a page that starts at its top (the empty
// AGENT tab, Help): ↑↓ scroll only while it runs past the screen,
// then where esc goes, if anywhere, and tab; help is "" on Help itself,
// where ? does nothing.
func pageFooter(more bool, esc, tab string, help bool) []FooterSeg {
	var out []FooterSeg
	if more {
		out = append(out, FooterSeg{Text: "↑↓ scroll"})
	}
	if esc != "" {
		out = append(out, FooterSeg{Text: esc})
	}
	out = append(out, FooterSeg{Text: tab})
	if help {
		out = append(out, helpSeg)
	}
	return out
}

// fieldLines is Plans' text field (docs/ux.md §2), shown only while it
// is open: from boxFrom rows a muted box titled with what it does
// ("╭ find ─"), else one row ("find  re"), the end of a long draft after
// "…". It reports where the terminal's cursor goes in it: rows under its
// top, and cells from the text column's edge.
func fieldLines(label, draft string, width int, boxed bool) (lines []LogLine, row, col int) {
	box := width - 3 // from column 2, one cell short of the edge
	if room := box - 4; boxed && room-cellLen(label) >= 1 {
		visible := visibleDraft(draft, room)
		top := "╭ " + label + " " + strings.Repeat("─", room-cellLen(label)) + "╮"
		bottom := "╰" + strings.Repeat("─", box-2) + "╯"
		mid := spanLine("muted", Span{Text: "  │ "}, Span{Text: visible, Role: "text"}, Span{Text: strings.Repeat(" ", room-cellLen(visible)) + " │"})
		return []LogLine{{Kind: "muted", Text: "  " + top}, mid, {Kind: "muted", Text: "  " + bottom}}, 1, 4 + cellLen(visible)
	}
	lead := "  " + label + "  "
	visible := visibleDraft(draft, max(0, width-1-cellLen(lead)))
	return []LogLine{spanLine("muted", Span{Text: lead}, Span{Text: visible, Role: "text"})}, 0, cellLen(lead) + cellLen(visible)
}
