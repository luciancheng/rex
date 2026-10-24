package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

// Span is one styled run of a line (docs/ux.md §5). Role and Fill name
// mascot.UI (or kindColor) roles; a span without them takes its line's
// Kind and Fill. Bold is the only attribute Rex adds. A line's Text stays
// the plain join of its spans, so text tests read it as before.
type Span struct {
	Text, Role, Fill string
	Bold             bool
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
// doesn't fit, and the rest is padded in fill.
func paintSpans(spans []Span, kind, fill string, width int, enabled bool) string {
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
		b.WriteString(paintRun(t, s, kind, fill, enabled))
		used += cellLen(t)
		if cut {
			break
		}
	}
	if used < width {
		b.WriteString(paintRun(strings.Repeat(" ", width-used), Span{}, kind, fill, enabled))
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

func paintRun(text string, s Span, kind, fill string, enabled bool) string {
	if text == "" {
		return ""
	}
	role, f := s.Role, s.Fill
	if role == "" {
		role = kind
	}
	if f == "" {
		f = fill
	}
	var bg *mascot.RGB
	if rgb, ok := mascot.UI[f]; ok {
		bg = &rgb
	} else if strings.TrimSpace(text) == "" && !s.Bold {
		return text // blank and unfilled: no color to set
	}
	fg := roleRGB(role)
	return mascot.PaintStyle(text, &fg, bg, s.Bold, enabled)
}

// pill marks the place or page you are on: "[WORD]" with the word in
// bold ink on fill and the brackets painted fill on fill, so in color
// they vanish and under NO_COLOR they still say "here" (docs/ux.md §5).
func pill(word, ink, fill string) []Span {
	return []Span{{Text: "[", Role: fill, Fill: fill}, {Text: word, Role: ink, Fill: fill, Bold: true}, {Text: "]", Role: fill, Fill: fill}}
}

// keySpans draws the keys in a row-1 line as keys, bold in text: a key
// word (footerKeys) that opens the line or follows " · ", as in
// "new plan cuj/next · enter switch" (docs/ux.md §4). A plan's name is
// never one: it never follows " · ".
func keySpans(text string) []Span {
	var spans []Span
	start := 0
	at := func(i int) {
		word, _, _ := strings.Cut(text[i:], " ")
		if !footerKeys[word] {
			return
		}
		if start < i {
			spans = append(spans, Span{Text: text[start:i]})
		}
		spans = append(spans, Span{Text: word, Role: "text", Bold: true})
		start = i + len(word)
	}
	at(0)
	for i := 0; ; {
		j := strings.Index(text[i:], " · ")
		if j < 0 {
			break
		}
		i += j + len(" · ")
		at(i)
	}
	if start < len(text) {
		spans = append(spans, Span{Text: text[start:]})
	}
	return spans
}

// h2Line is a section header (docs/ux.md §5): the label in bold capitals
// at column 0, a slate rule to the edge, then optional muted meta.
func h2Line(label, meta string, width int) LogLine {
	label, meta = flat(label), flat(meta)
	rule := width - cellLen(label) - 1
	if meta != "" {
		if rule-cellLen(meta)-1 < 3 {
			meta = ""
		} else {
			rule -= cellLen(meta) + 1
		}
	}
	spans := []Span{{Text: label, Role: "text", Bold: true}, {Text: " " + strings.Repeat("─", max(3, rule)), Role: "border"}}
	if meta != "" {
		spans = append(spans, Span{Text: " " + meta, Role: "muted"})
	}
	return spanLine("text", spans...)
}

// h3 is a field (docs/ux.md §5, H3): a muted lowercase label and the
// items it holds. With no label it is the text under an H2.
type h3 struct {
	label string
	items []string
	role  string // the values' role; "" is text
	list  bool   // "• " bullets even for one item, as the field beside it has
	bare  bool   // never "• " bullets: one row per item, or items that carry their own glyph
	rows  int    // the rows each item may take, the last cut with "…"; 0 is all
}

// h3Col is the label column for fields shown together: 8 cells, wider
// where a label needs two cells after it ("changed  ").
func h3Col(fields ...h3) int {
	col := 8
	for _, f := range fields {
		if len(f.items) > 0 {
			col = max(col, cellLen(f.label)+2)
		}
	}
	return col
}

// fieldLines draws a field from column 2: its label in a col-cell column
// and its value beside it, wrapped values (and each line of one, as the
// file breaks it) hanging under it. Several items get "• " bullets; paths
// go one per line, bare. Each line's first span is its 2-cell lead, which
// the PINNED card swaps for its edge.
func fieldLines(f h3, width, col int) []LogLine {
	if len(f.items) == 0 {
		return nil
	}
	paths := true
	for _, it := range f.items {
		if strings.ContainsAny(it, " \t\n") || !strings.Contains(it, "/") {
			paths = false
		}
	}
	bullet := (len(f.items) > 1 || f.list) && !paths && !f.bare
	room := max(1, width-2-col)
	label := Span{Text: padRight(flat(f.label), col), Role: "muted"}
	var lines []LogLine
	for _, it := range f.items {
		mark, hang := "", ""
		if bullet {
			mark, hang = "• ", "  "
		}
		for i, row := range paraRows(it, max(1, room-cellLen(mark)), f.rows) {
			text := hang + row
			if i == 0 {
				text = mark + row
			}
			lines = append(lines, spanLine("text", Span{Text: "  "}, label, Span{Text: text, Role: f.role}))
			label = Span{Text: strings.Repeat(" ", col)}
		}
	}
	return lines
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
// keeps the spaces inside a row: "✓ plans-ui  Home cards". Each row costs
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

// Page is a screen's page title, row 2 (docs/ux.md §4): a breadcrumb that
// is also the Esc ladder (Esc goes to the crumb on the left), and meta on
// the right. MetaShort stands in for a Meta that doesn't fit, as the list's
// position does for "active · 16 of 30". Selected is the outline's title
// row under the cursor.
type Page struct {
	Crumbs                    []string
	Meta, MetaShort, MetaRole string
	Selected                  bool
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

// fitCrumbs fits a page title into width cells (docs/ux.md §4): the meta
// drops first (the first of metas that fits whole stays), then middle
// crumbs clip to crumbMin cells each, then the root becomes "…", then
// middle crumbs fold into it while the current one would get fewer than
// crumbMin cells; what still doesn't fit is clipped at the end, so only
// the current crumb is ever cut short. A crumb is never re-cased.
func fitCrumbs(crumbs []string, width int, metas ...string) ([]string, string) {
	c := make([]string, len(crumbs))
	for i, s := range crumbs {
		c[i] = flat(s)
	}
	room := width - 2
	joined := func() int {
		n := 0
		for i, s := range c {
			if i > 0 {
				n += cellLen(crumbSep)
			}
			n += cellLen(s)
		}
		return n
	}
	for _, meta := range metas {
		if meta = flat(meta); meta != "" && joined()+2+cellLen(meta)+1 <= room {
			return c, meta
		}
	}
	for i := 1; i < len(c)-1; i++ {
		if over := joined() - room; over > 0 {
			c[i] = ellipsis(c[i], max(crumbMin, cellLen(c[i])-over))
		}
	}
	if len(c) > 1 && joined() > room {
		c[0] = "…"
	}
	// Help over a task has four crumbs: the middle ones fold into the "…"
	// before the current one is left fewer than crumbMin cells.
	for len(c) > 2 && room-(joined()-cellLen(c[len(c)-1])) < min(crumbMin, cellLen(c[len(c)-1])) {
		c = append(c[:1], c[2:]...)
	}
	return c, ""
}

const crumbSep = " › "

// pageLine draws a page title as a full-width slate bar (the selection's
// fill when the outline's title row is selected): crumbs from column 2,
// the current one bold, and the meta ending one cell from the edge.
// Under NO_COLOR it is the same text with no bar.
func pageLine(p Page, width int) LogLine {
	crumbs, meta := fitCrumbs(p.Crumbs, width, p.Meta, p.MetaShort)
	fill, lead := "border", Span{Text: "  "}
	if p.Selected {
		fill, lead = "pickbg", Span{Text: "▌ ", Role: "pick"}
	}
	spans := []Span{lead}
	for i, c := range crumbs {
		if i > 0 {
			spans = append(spans, Span{Text: crumbSep})
		}
		spans = append(spans, Span{Text: c, Bold: i == len(crumbs)-1})
	}
	if meta != "" {
		role := p.MetaRole
		if role == "" {
			role = "text"
		}
		gap := width - cellLen(spanText(spans)) - cellLen(meta) - 1
		spans = append(spans, Span{Text: strings.Repeat(" ", gap)}, Span{Text: meta, Role: role}, Span{Text: " "})
	}
	line := spanLine("text", spans...)
	line.Fill = fill
	return line
}

// FooterSeg is one footer segment, a key and what it does: "enter open".
// Short is what it shrinks to when it is the last one that fits: "?".
type FooterSeg struct{ Text, Short string }

func segs(texts ...string) []FooterSeg {
	out := make([]FooterSeg, len(texts))
	for i, t := range texts {
		out[i] = FooterSeg{Text: t}
	}
	return out
}

// helpSeg is "? help", "?" when it is the last thing that fits.
var helpSeg = FooterSeg{Text: "? help", Short: "?"}

// fitFooter keeps the longest prefix of segs that fits width cells, joined
// by two spaces (docs/ux.md §4): rank is display order. Before the last
// kept segment drops, its short form is tried. Words are never cut, except
// the first segment on a terminal too narrow even for it.
func fitFooter(segs []FooterSeg, width int) []string {
	for n := len(segs); n > 0; n-- {
		kept := make([]string, n)
		for i, s := range segs[:n] {
			kept[i] = s.Text
		}
		if cellLen(strings.Join(kept, "  ")) <= width {
			return kept
		}
		if s := segs[n-1].Short; s != "" {
			if kept[n-1] = s; cellLen(strings.Join(kept, "  ")) <= width {
				return kept
			}
		}
	}
	if len(segs) == 0 {
		return nil
	}
	return []string{strings.TrimRight(clip(segs[0].Text, width), " ")}
}

// footerKeys are the words a footer draws as keys.
var footerKeys = map[string]bool{"enter": true, "esc": true, "tab": true, "?": true, "↑↓": true, "→": true, "home": true, "end": true, "ctrl+c": true}

// footerLine draws the footer: each segment's key bold, what it does
// muted (the quit warning keeps its error color).
func footerLine(segs []FooterSeg, width int, kind string) LogLine {
	key := "text"
	if kind == "error" {
		key = "error"
	}
	var spans []Span
	for i, t := range fitFooter(segs, width) {
		if i > 0 {
			spans = append(spans, Span{Text: "  "})
		}
		word, _, _ := strings.Cut(t, " ")
		if footerKeys[word] {
			spans = append(spans, Span{Text: word, Role: key, Bold: true})
			t = t[len(word):]
		}
		if t != "" {
			spans = append(spans, Span{Text: t})
		}
	}
	return spanLine(kind, spans...)
}
