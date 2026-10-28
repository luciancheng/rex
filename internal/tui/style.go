package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

// Span is one styled run of a line (docs/ux.md §5). Role names a
// mascot.UI (or kindColor) role; a span without one takes its line's
// Kind. Bold is the only attribute Rex adds, and nothing is filled. A
// line's Text stays the plain join of its spans, so text tests read it as
// before.
type Span struct {
	Text, Role string
	Bold       bool
}

func spanText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// spanLine is a line drawn from spans.
func spanLine(kind string, spans ...Span) LogLine {
	return LogLine{Kind: kind, Text: spanText(spans), Spans: spans}
}

// flat is s as one line of clean text, measured as clip will paint it.
func flat(s string) string {
	return strings.ReplaceAll(watch.CleanText(s), "\n", " ")
}

// flatHead is flat(s) cut to at most cells cells. It reads only as much of
// s as that takes, so a 600-byte title on a row costs the row's cells.
func flatHead(s string, cells int) string {
	for n := max(1, cells+1); n < len(s); n *= 2 {
		// A prefix cleans to a prefix of the whole: a sequence or a rune it
		// cuts is dropped at its end.
		if f := flat(s[:n]); cellLen(f) > cells {
			return cutCells(f, cells)
		}
	}
	return cutCells(flat(s), cells)
}

// flatJoin is items joined with " · " as flat text, cut to at most cells
// cells: it reads only the items, and as much of each, as that takes.
func flatJoin(items []string, cells int) string {
	var b strings.Builder
	n := 0
	for i, s := range items {
		if n > cells {
			break
		}
		if i > 0 {
			b.WriteString(" · ")
			n += 3
		}
		s = flatHead(s, max(0, cells-n)+1)
		b.WriteString(s)
		n += cellLen(s)
	}
	return cutCells(b.String(), cells)
}

// roleRGB is a role's color: mascot.UI, then an AGENT block kind's, else
// text.
func roleRGB(role string) mascot.RGB {
	if rgb, ok := mascot.UI[role]; ok {
		return rgb
	}
	if name := kindColor[role]; name != "" {
		return mascot.UI[name]
	}
	return mascot.UI["text"]
}

// paintSpans paints spans into exactly width cells, as clip would: each
// span is cleaned, the whole is cut by cells with a trailing "…" when it
// doesn't fit, and the rest is padded with blanks.
func paintSpans(spans []Span, kind string, width int, enabled bool) string {
	if width <= 0 {
		return ""
	}
	texts := make([]string, len(spans))
	total := 0
	for i, s := range spans {
		texts[i] = flat(s.Text)
		total += cellLen(texts[i])
	}
	room := width
	if total > width {
		room = width - 1 // for the "…"
	}
	var b strings.Builder
	used := 0
	for i, s := range spans {
		t := texts[i]
		cut := used+cellLen(t) > room
		if cut {
			t = cutCells(t, room-used) + "…"
		}
		b.WriteString(paintRun(t, s, kind, enabled))
		used += cellLen(t)
		if cut {
			break
		}
	}
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	return b.String()
}

// spansLen is the cells spans take, as clip measures them.
func spansLen(spans []Span) int {
	n := 0
	for _, s := range spans {
		n += cellLen(flat(s.Text))
	}
	return n
}

// cutSpans cuts spans to width cells with a trailing "…" when they don't
// fit, as clip does a string; the "…" takes the cut span's style.
func cutSpans(spans []Span, width int) []Span {
	if spansLen(spans) <= width {
		return spans
	}
	var out []Span
	used := 0
	for _, s := range spans {
		s.Text = flat(s.Text)
		if n := cellLen(s.Text); used+n > width-1 {
			s.Text = cutCells(s.Text, max(0, width-1-used)) + "…"
			return append(out, s)
		}
		out = append(out, s)
		used += cellLen(s.Text)
	}
	return out
}

// padSpans pads spans with blanks to width cells.
func padSpans(spans []Span, width int) []Span {
	if n := spansLen(spans); n < width {
		return append(spans, Span{Text: strings.Repeat(" ", width-n)})
	}
	return spans
}

// sideSpans fits left and a right-hand meta into width cells, the meta
// ending at the edge: left clips first, and the meta goes only when the
// left would keep fewer than 6 cells.
func sideSpans(left []Span, meta Span, width int) []Span {
	room := width - cellLen(meta.Text) - 2
	if meta.Text == "" || room < 6 {
		return cutSpans(left, width)
	}
	return append(padSpans(cutSpans(left, room), width-cellLen(meta.Text)), meta)
}

// doneMark is done (docs/ux.md §5), the one thing green paints: it keeps
// its green in any run, the selection's included.
const doneMark = "✔"

func paintRun(text string, s Span, kind string, enabled bool) string {
	if text == "" || strings.TrimSpace(text) == "" && !s.Bold {
		return text // blank: no color to set
	}
	role := s.Role
	if role == "" {
		role = kind
	}
	if i := strings.Index(text, doneMark); i >= 0 && role != "action" {
		done := Span{Text: doneMark, Role: "action", Bold: s.Bold}
		return paintRun(text[:i], s, kind, enabled) + paintRun(doneMark, done, kind, enabled) + paintRun(text[i+len(doneMark):], s, kind, enabled)
	}
	return mascot.PaintStyle(text, roleRGB(role), s.Bold, enabled)
}

// selMark is the selection's gutter (docs/ux.md §5): ❯ and a blank, on
// one row of a screen.
const selMark = "❯ "

// picked is a row's spans as the selection draws them (docs/ux.md §5):
// in the line's kind, the selection's purple, but for the marks that keep
// their meaning there, the pin's yellow and an error's red (✔ keeps its
// green in paintRun).
func picked(spans []Span) []Span {
	out := make([]Span, len(spans))
	for i, s := range spans {
		if s.Role != "accent" && s.Role != "error" {
			s.Role = ""
		}
		out[i] = s
	}
	return out
}

// paraRows is a value wrapped to width cells, each line of it starting a
// row, so a description's paragraphs and lists keep their breaks; blank
// lines are dropped. most caps the rows of the whole value: the line that
// reaches it takes the rest of the value with it, the last row cut with
// "…" when there is more. A capped value is wrapped only as far as the cap.
func paraRows(text string, width, most int) []string {
	var paras []string
	for _, para := range strings.Split(watch.CleanText(text), "\n") {
		if para = strings.TrimSpace(para); para != "" {
			paras = append(paras, para)
		}
	}
	var rows []string
	for i, para := range paras {
		left := 0
		if most > 0 {
			left = most - len(rows)
		}
		wrapped := wrapMost(para, width, left)
		if most > 0 && len(wrapped) >= left {
			return append(rows, wrapMost(strings.Join(paras[i:], " "), width, left)...)
		}
		rows = append(rows, wrapped...)
	}
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

// wrapMost wraps text to width cells in at most most rows (0: as many as
// it takes), the last cut with "…" when there is more. Unlike wrap it
// keeps the spaces inside a row: "✔ plans-ui  Home cards". Each row costs
// its own cells, never the rest of the text's.
func wrapMost(text string, width, most int) []string {
	if width <= 0 {
		return nil
	}
	text = strings.TrimSpace(text)
	var rows []string
	for text != "" {
		if most > 0 && len(rows) == most-1 {
			rows = append(rows, ellipsis(text, width))
			break
		}
		var row string
		row, text = cutRow(text, width)
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

// cutRow is text's first row of at most width cells and the rest of the
// text after it. A row breaks at a space, a space just past it included,
// or after a wide glyph, as CJK text breaks; a run of narrow letters
// longer than the row breaks inside it.
func cutRow(text string, width int) (row, rest string) {
	fit := cutCells(text, width)
	if len(fit) == len(text) {
		return text, ""
	}
	at, n, glyph := 0, 0, 0 // the last break; the cells before r; the width of the glyph before r
	var before, prev rune
	for i, r := range text {
		if n > width {
			break
		}
		w := stepWidth(before, prev, r)
		if i > 0 && w > 0 && !joiner(prev, r) {
			if r == ' ' || glyph == 2 {
				at = i
			}
			glyph = 0
		}
		n, glyph = n+w, glyph+w
		before, prev = prev, r
	}
	if at == 0 {
		if at = len(fit); at == 0 {
			_, at = utf8.DecodeRuneInString(text)
		}
	}
	return strings.TrimRight(text[:at], " "), strings.TrimLeft(text[at:], " ")
}

// Page is a screen's title, row 0 (docs/ux.md §4): a breadcrumb that is
// also the Esc ladder (Esc goes to the crumb on the left), and meta on
// the right, muted unless a span says otherwise. Short stands in for a
// Meta that doesn't fit, as the progress "1/3" does for "1/3 done ·
// following". The cursor never reaches it.
type Page struct {
	Crumbs      []string
	Meta, Short []Span
}

// metaSpans is meta text in its muted role.
func metaSpans(text string) []Span {
	if text == "" {
		return nil
	}
	return []Span{{Text: text, Role: "muted"}}
}

// crumbMin is the fewest cells a middle crumb clips to: "Send-…".
const crumbMin = 6

// ellipsis is s cut to width cells with a trailing "…" when it doesn't
// fit, unpadded (clip pads).
func ellipsis(s string, width int) string {
	cut := cutCells(s, width)
	switch {
	case len(cut) == len(s):
		return s
	case width <= 1:
		return cutCells("…", width)
	}
	return strings.TrimRight(cutCells(cut, width-1), " ") + "…"
}

const crumbSep = " › "

// crumbsLen is the cells crumbs take, joined.
func crumbsLen(c []string) int {
	n := 0
	for i, s := range c {
		if i > 0 {
			n += cellLen(crumbSep)
		}
		n += cellLen(s)
	}
	return n
}

// fitCrumbs fits a breadcrumb into room cells (docs/ux.md §4): middle
// crumbs clip to crumbMin cells each, then the root becomes "…", then the
// middle crumbs fold into it, then only the current crumb is left. It
// reports false when even that doesn't fit whole. A crumb is never
// re-cased.
func fitCrumbs(crumbs []string, room int) ([]string, bool) {
	c := make([]string, len(crumbs))
	for i, s := range crumbs {
		c[i] = flat(s)
	}
	if crumbsLen(c) <= room {
		return c, true
	}
	for i := 1; i < len(c)-1; i++ {
		if over := crumbsLen(c) - room; over > 0 {
			c[i] = ellipsis(c[i], max(crumbMin, cellLen(c[i])-over))
		}
	}
	if crumbsLen(c) <= room {
		return c, true
	}
	if len(c) > 1 {
		c[0] = "…"
	}
	for len(c) > 2 && crumbsLen(c) > room {
		c = append(c[:1], c[2:]...)
	}
	if crumbsLen(c) <= room {
		return c, true
	}
	last := c[len(c)-1:]
	return last, cellLen(last[0]) <= room
}

// titleLine draws row 0 (docs/ux.md §4): the breadcrumb from column 0,
// the current crumb a bright bold title and the ones before it muted; the
// meta; and, three cells after it, the places' label, all ending one cell
// from the edge. Short of room, the breadcrumb clips first, down to its
// current crumb; then the meta goes (its short form first); then the label
// shortens to the place on screen; under labelFrom cells there is no
// label; and only then is the current crumb cut, to the room left.
func titleLine(p Page, width int, pane PaneKind) LogLine {
	room := width - 1
	var metas [][]Span
	for _, m := range [][]Span{p.Meta, p.Short} {
		if len(m) > 0 {
			metas = append(metas, m)
		}
	}
	type form struct{ meta, label []Span }
	var forms []form
	if width >= labelFrom {
		for _, m := range metas {
			forms = append(forms, form{m, placeLabel(pane, false)})
		}
		forms = append(forms, form{nil, placeLabel(pane, false)}, form{nil, placeLabel(pane, true)})
	} else {
		for _, m := range metas {
			forms = append(forms, form{m, nil})
		}
		forms = append(forms, form{nil, nil})
	}
	side := func(f form) int {
		n := 0
		if len(f.meta) > 0 {
			n += 2 + spansLen(f.meta)
		}
		if len(f.label) > 0 {
			n += 3 + spansLen(f.label)
		}
		return n
	}
	// When nothing fits whole, the last form's current crumb is cut to the
	// room it leaves: from labelFrom cells the label stays, short.
	pick := forms[len(forms)-1]
	var crumbs []string
	if n := len(p.Crumbs); n > 0 {
		crumbs = []string{ellipsis(flat(p.Crumbs[n-1]), max(1, room-side(pick)))}
	}
	for _, f := range forms {
		if c, ok := fitCrumbs(p.Crumbs, room-side(f)); ok {
			pick, crumbs = f, c
			break
		}
	}
	var left []Span
	for i, c := range crumbs {
		if i > 0 {
			left = append(left, Span{Text: crumbSep, Role: "muted"})
		}
		if i == len(crumbs)-1 {
			left = append(left, Span{Text: c, Role: "title", Bold: true})
		} else {
			left = append(left, Span{Text: c, Role: "muted"})
		}
	}
	var right []Span
	if len(pick.meta) > 0 {
		right = append(right, pick.meta...)
	}
	if len(pick.label) > 0 {
		if len(right) > 0 {
			right = append(right, Span{Text: "   "})
		}
		right = append(right, pick.label...)
	}
	spans := left
	if len(right) > 0 {
		gap := room - spansLen(left) - spansLen(right)
		spans = append(append(spans, Span{Text: strings.Repeat(" ", max(2, gap))}), right...)
	}
	return spanLine("text", spans...)
}

// FooterSeg is one footer segment, a key and what it does: "enter open"
// (docs/ux.md §4). Wide is its longest form on this screen ("enter
// create"): the footer is fitted by it, so moving the cursor never changes
// which segments show. Rank, when set, outranks the key's own (1 is a
// must-have).
type FooterSeg struct {
	Text, Wide string
	Rank       int
}

// seg is a segment that reads text now and may read any of variants as
// the cursor moves.
func seg(text string, variants ...string) FooterSeg {
	s := FooterSeg{Text: text, Wide: text}
	for _, v := range variants {
		if cellLen(v) > cellLen(s.Wide) {
			s.Wide = v
		}
	}
	return s
}

func segs(texts ...string) []FooterSeg {
	out := make([]FooterSeg, len(texts))
	for i, t := range texts {
		out[i] = FooterSeg{Text: t}
	}
	return out
}

// helpSeg is "? help", always last and always kept.
var helpSeg = FooterSeg{Text: "? help"}

// footerRank is which segments the footer keeps longest, by key (docs/ux.md
// §4), after ? help: enter, p, esc, tab, d, ↑↓, typing, then a scroll's
// ends; i about and anything else go first.
var footerRank = map[string]int{"ctrl+c": 1, "enter": 2, "p": 3, "esc": 4, "tab": 5, "d": 6, "↑↓": 7, "type": 8, "end": 9, "home": 10}

func (s FooterSeg) rank() int {
	if s.Rank != 0 {
		return s.Rank
	}
	key, _, _ := strings.Cut(s.Text, " ")
	if r, ok := footerRank[key]; ok {
		return r
	}
	return len(footerRank) + 1
}

func (s FooterSeg) wide() int {
	return max(cellLen(s.Text), cellLen(s.Wide))
}

// footerSep joins the footer's segments.
const footerSep = " · "

// fitFooter keeps what of segs fits width cells, joined by " · "
// (docs/ux.md §4): while they don't fit, the lowest-ranked segment goes
// (the later of a tie), never ? help, and the order never changes. Each segment counts
// as its widest form, so the cursor never changes what fits. Words are
// never cut, except the last segment left on a terminal too narrow even
// for it.
func fitFooter(segs []FooterSeg, width int) []string {
	kept := append([]FooterSeg(nil), segs...)
	total := func() int {
		n := 0
		for i, s := range kept {
			if i > 0 {
				n += cellLen(footerSep)
			}
			n += s.wide()
		}
		return n
	}
	for len(kept) > 1 && total() > width {
		drop := -1
		for i, s := range kept {
			if s != helpSeg && (drop < 0 || s.rank() >= kept[drop].rank()) {
				drop = i
			}
		}
		kept = append(kept[:drop], kept[drop+1:]...)
	}
	if len(kept) == 0 {
		return nil
	}
	out := make([]string, len(kept))
	for i, s := range kept {
		out[i] = s.Text
	}
	if len(out) == 1 && cellLen(out[0]) > width {
		out[0] = strings.TrimRight(clip(out[0], width), " ")
	}
	return out
}

// footerLine draws the footer (docs/ux.md §4): one muted line of "key
// verb" segments, one cell short of the edge. The quit warning is muted
// too.
func footerLine(segs []FooterSeg, width int) LogLine {
	return LogLine{Kind: "muted", Text: strings.Join(fitFooter(segs, width-1), footerSep)}
}
