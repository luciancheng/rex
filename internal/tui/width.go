package tui

import (
	"strings"
	"unicode"

	"github.com/luciancheng/rex/internal/watch"
)

// Width in terminal cells. The standard library has no East Asian Width
// table and Rex adds no modules, so this is a small hand-kept range list:
// wide and fullwidth CJK, Hangul and emoji count 2, combining marks and
// zero-width characters count 0, and everything else, including the
// glyphs Rex draws (· … ─ │ ❯ ✔ ✗ › ○ ↑ ↓), counts 1.
//
// Strings are measured by sequence, the way terminals draw them: U+FE0F
// (emoji presentation) or a keycap makes the symbol before it 2 cells
// (⚠️ ✔️ 1️⃣), and a skin tone or anything after a ZWJ joins the glyph
// before it (👍🏽 👨‍💻 are 2). Pictographs some terminals draw narrow
// (🖥 🛠 without U+FE0F) still count 2; the renderer erases each row it
// repaints, so a narrower glyph never leaves stale cells.

var wideRanges = [][2]rune{
	{0x1100, 0x115f}, {0x231a, 0x231b}, {0x2329, 0x232a}, {0x23e9, 0x23ec}, {0x23f0, 0x23f0},
	{0x23f3, 0x23f3}, {0x25fd, 0x25fe}, {0x2614, 0x2615}, {0x2648, 0x2653}, {0x267f, 0x267f},
	{0x2693, 0x2693}, {0x26a1, 0x26a1}, {0x26aa, 0x26ab}, {0x26bd, 0x26be}, {0x26c4, 0x26c5},
	{0x26ce, 0x26ce}, {0x26d4, 0x26d4}, {0x26ea, 0x26ea}, {0x26f2, 0x26f3}, {0x26f5, 0x26f5},
	{0x26fa, 0x26fa}, {0x26fd, 0x26fd}, {0x2705, 0x2705}, {0x270a, 0x270b}, {0x2728, 0x2728},
	{0x274c, 0x274c}, {0x274e, 0x274e}, {0x2753, 0x2755}, {0x2757, 0x2757}, {0x2795, 0x2797},
	{0x27b0, 0x27b0}, {0x27bf, 0x27bf}, {0x2b1b, 0x2b1c}, {0x2b50, 0x2b50}, {0x2b55, 0x2b55},
	{0x2e80, 0x303e}, {0x3041, 0x33ff}, {0x3400, 0x4dbf}, {0x4e00, 0x9fff}, {0xa000, 0xa4cf},
	{0xa960, 0xa97f}, {0xac00, 0xd7a3}, {0xf900, 0xfaff}, {0xfe10, 0xfe19}, {0xfe30, 0xfe6f},
	{0xff00, 0xff60}, {0xffe0, 0xffe6}, {0x16fe0, 0x16fe4}, {0x17000, 0x18d08}, {0x1b000, 0x1b2ff},
	{0x1f004, 0x1f004}, {0x1f0cf, 0x1f0cf}, {0x1f18e, 0x1f18e}, {0x1f191, 0x1f19a}, {0x1f200, 0x1f2ff},
	{0x1f300, 0x1f64f}, {0x1f680, 0x1f6ff}, {0x1f7e0, 0x1f7eb}, {0x1f90c, 0x1f9ff}, {0x1fa70, 0x1faff},
	{0x20000, 0x2fffd}, {0x30000, 0x3fffd},
}

func cellWidth(r rune) int {
	switch {
	case r >= 0x20 && r < 0x7f:
		return 1 // ASCII: the common case, no table lookup
	case r == 0:
		return 0
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r < 0x300:
		return 1 // no combining marks below U+0300
	case r < 0x1100:
		if unicode.In(r, unicode.Mn, unicode.Me) {
			return 0
		}
		return 1
	case r == 0x200b || r == 0x200c || r == 0x200d || r == 0xfeff || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0xe0100 && r <= 0xe01ef):
		return 0
	case unicode.In(r, unicode.Mn, unicode.Me):
		return 0
	}
	lo, hi := 0, len(wideRanges)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch {
		case r < wideRanges[mid][0]:
			hi = mid - 1
		case r > wideRanges[mid][1]:
			lo = mid + 1
		default:
			return 2
		}
	}
	return 1
}

// stepWidth is the cells r adds after prev (0 at the start of the text);
// before is the glyph ahead of prev, which a ZWJ joins r to.
func stepWidth(before, prev, r rune) int {
	switch {
	case r == 0xfe0f || r == 0x20e3:
		if prev != 0 && cellWidth(prev) == 1 {
			return 1 // emoji presentation or keycap: the symbol is 2 cells
		}
		return 0
	case r >= 0x1f3fb && r <= 0x1f3ff && prev != 0 && cellWidth(prev) == 2:
		return 0 // skin tone on the emoji before it
	case prev == 0x200d && pictograph(before) && pictograph(r):
		return 0 // 👨‍💻: one glyph
	}
	return cellWidth(r)
}

// pictograph is an emoji a ZWJ can join: the emoji blocks, and the
// symbols U+FE0F turns into emoji.
func pictograph(r rune) bool {
	return (r >= 0x1f000 && r <= 0x1faff) || (r >= 0x2600 && r <= 0x27bf)
}

// joiner is a rune that only modifies or joins the glyph before it, so a
// cut must not start on it.
func joiner(prev, r rune) bool {
	return r == 0xfe0f || r == 0x20e3 || r == 0x200d || (r >= 0x1f3fb && r <= 0x1f3ff) || prev == 0x200d
}

// cellLen is the width of s in terminal cells.
func cellLen(s string) int {
	n := 0
	var before, prev rune
	for _, r := range s {
		n += stepWidth(before, prev, r)
		before, prev = prev, r
	}
	return n
}

// cutCells returns the longest prefix of s that fits in width cells.
func cutCells(s string, width int) string {
	n := 0
	var before, prev rune
	for i, r := range s {
		w := stepWidth(before, prev, r)
		if n+w > width {
			return s[:i]
		}
		n += w
		before, prev = prev, r
	}
	return s
}

// tailCells returns the longest suffix of s that fits in width cells. It
// never starts inside a sequence (on a joiner or a modifier).
func tailCells(s string, width int) string {
	r := []rune(s)
	w := make([]int, len(r))
	var before, prev rune
	for i, c := range r {
		w[i] = stepWidth(before, prev, c)
		before, prev = prev, c
	}
	n, start := 0, 0
	for i := len(r) - 1; i >= 0; i-- {
		if n+w[i] > width {
			start = i + 1
			break
		}
		n += w[i]
	}
	for start > 0 && start < len(r) && (w[start] == 0 || joiner(r[start-1], r[start])) {
		start++
	}
	return string(r[start:])
}

// clip is the one place text is fitted to a width before it is painted:
// it cleans the text, cuts it by cells with a trailing "…" when it had to
// cut, and pads with spaces to exactly width cells.
func clip(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = strings.ReplaceAll(watch.CleanText(text), "\n", " ")
	if n := cellLen(text); n <= width {
		return text + strings.Repeat(" ", width-n)
	}
	if width == 1 {
		return "…"
	}
	cut := cutCells(text, width-1) + "…"
	return cut + strings.Repeat(" ", width-cellLen(cut))
}

func padRight(s string, width int) string {
	n := cellLen(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
}
