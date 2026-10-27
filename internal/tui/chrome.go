package tui

import (
	"fmt"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
)

// TextOnlyBelow is the width under which the mascot is hidden so the text
// column keeps the whole terminal (docs/ux.md §12).
const TextOnlyBelow = 40 + Gutter + MinTextCols

// maxText is the most of a wide terminal the text column takes (docs/ux.md
// §4): wider, the rest of the row stays empty.
const maxText = 100

// blankFrom is the height from which row 2 is blank and the notice row
// is H−2 (docs/ux.md §4); AGENT puts blank rows between its blocks from
// there too (§10). Shorter, a notice takes row 1 while it shows.
const blankFrom = 12

// layout is how a terminal size splits into mascot, text and composer.
type layout struct {
	mascotCols int // 0 in text-only mode
	gutter     int
	textW      int // 0 when the text column would be too narrow
	blank      int // 1 when row 2 is the blank row (docs/ux.md §4)
	notice     int // 1 when H−2 is the notice row
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
	l.textW = min(l.textW, maxText)
	switch {
	case lines >= 16:
		l.composer = InputRows
	case lines >= 7:
		l.composer = 1
	}
	if lines >= blankFrom {
		l.blank, l.notice = 1, 1
	}
	l.typing = l.composer > 0 && !l.locked
	l.bodyH = max(0, lines-2-l.blank-l.notice-FooterRows-l.composer)
	return l
}

// bare is l on a screen without a composer: the body takes its rows.
// typing still says whether the terminal is tall enough to type in.
func (l layout) bare() layout {
	l.bodyH += l.composer
	l.composer = 0
	return l
}

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

// withoutEnter is the plan list's footer when the screen is too short to
// show its composer (docs/ux.md §12). Typing is ignored there, so "type
// to …" goes; enter never submits a draft blind, so enter goes while the
// draft has text, and in a Name prompt, which nothing can be typed into;
// with a hidden draft ? would be text, so "? help" goes too. None of it
// depends on the cursor. Screens without a composer keep their footers,
// since their keys act at once.
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

// hintText is how a placeholder reads: dim when there is color, in
// parentheses when there isn't, so it never looks like typed text.
func hintText(placeholder string, enabled bool) string {
	if enabled || placeholder == "" {
		return placeholder
	}
	return "(" + placeholder + ")"
}

// enterOn is what enter does on the plan list's selected row, as the
// footer says it: it opens a plan or a folder, or starts or makes a new
// one. Its width is the longer, so the cursor never changes the footer.
func enterOn(b *PlanBrowser) FooterSeg {
	s := seg("enter open", "enter create")
	if b.Cursor < len(b.Rows) {
		switch b.Rows[b.Cursor].Kind {
		case PlanRowNew, PlanRowMkdir, PlanRowCreateNamed:
			s.Text = "enter create"
		}
	}
	return s
}

// listFooter is the plan list's footer (docs/ux.md §4): ↑↓, what enter
// does on the selected row, typing, where esc goes, tab, then ? help while
// ? opens Help (on an empty draft).
func listFooter(b *PlanBrowser, draft, preview string) []FooterSeg {
	tab := FooterSeg{Text: "tab agent"}
	move := FooterSeg{Text: "↑↓ select"}
	if preview != "" {
		return []FooterSeg{{Text: preview}, {Text: "esc clear"}, tab}
	}
	enter := enterOn(b)
	switch {
	case b.Prompting() && draft == "":
		return []FooterSeg{{Text: "enter create"}, {Text: "esc cancel"}, tab, helpSeg}
	case b.Prompting():
		return []FooterSeg{{Text: "enter create"}, {Text: "esc clear"}, tab}
	case draft != "":
		return []FooterSeg{move, enter, {Text: "esc clear"}, tab}
	case b.Browsing():
		esc := seg("esc up", "esc plans")
		if b.topFolder() {
			esc.Text = "esc plans" // from a top folder Esc is the plan list (docs/ux.md §11)
		}
		return []FooterSeg{move, enter, {Text: "type to filter"}, esc, tab, helpSeg}
	case b.Current == "":
		// With no plan open, Esc goes back to the Wait screen (docs/ux.md §3).
		return []FooterSeg{move, enter, {Text: "type to find"}, {Text: "esc wait"}, tab, helpSeg}
	}
	return []FooterSeg{move, enter, {Text: "type to find"}, tab, helpSeg}
}

// agentFooter is AGENT's footer (docs/ux.md §4): scrolling, its ends,
// Tab and Help. A new plan's offer is taken with o, which its notice
// names, never the footer.
func agentFooter() []FooterSeg {
	return []FooterSeg{{Text: "↑↓ scroll"}, {Text: "home oldest"}, {Text: "end live"}, {Text: "tab plan"}, helpSeg}
}

// pageFooter is the footer of a page that starts at its top (Wait, the
// empty AGENT tab, Help): ↑↓ scroll only while it runs past the screen,
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

// cursorFor places the cursor after the draft, or hides it (row 0). The
// composer sits over the notice row and the footer.
func cursorFor(l layout, lines int, draft, prefix string) (int, int) {
	visible := visibleDraft(draft, l.textW-l.composerChrome()-cellLen(prefix))
	switch l.composer {
	case InputRows:
		return lines - 2 - l.notice, l.mascotCols + l.gutter + 1 + 1 + cellLen(prefix) + cellLen(visible)
	case 1:
		return lines - 1 - l.notice, l.mascotCols + l.gutter + 1 + cellLen(prefix) + cellLen(visible)
	}
	return 0, 0
}

func (l layout) composerChrome() int {
	if l.composer == InputRows {
		return 2
	}
	return 0
}
