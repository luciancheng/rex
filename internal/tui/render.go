package tui

import (
	"fmt"
	"strings"
)

const (
	syncBegin = "\033[?2026h" // synchronized output; terminals without it ignore it
	syncEnd   = "\033[?2026l"
	eraseLine = "\033[2K" // the whole row, from column 1: no deferred-wrap trap
)

// renderer paints frames without clearing the screen: only rows that
// changed are rewritten, inside a synchronized update, with the cursor
// hidden while painting. A full repaint (clear + every row) happens on the
// first frame, after a resize, and when asked (Ctrl+L, SIGCONT).
type renderer struct {
	prev []string
	full bool
}

func (r *renderer) invalidate() { r.full = true }

func (r *renderer) render(f Frame) string {
	full := r.full || len(r.prev) != len(f.Rows)
	if !full {
		for i := range f.Rows {
			if cellLen(stripSGR(f.Rows[i])) != cellLen(stripSGR(r.prev[i])) {
				full = true
				break
			}
		}
	}
	var prev []string
	if !full {
		prev = r.prev
	}
	out := diffFrame(prev, f, full)
	r.prev = append(r.prev[:0], f.Rows...)
	r.full = false
	return out
}

// diffFrame writes the rows of next that differ from prev (all of them when
// prev is nil). Each changed row is erased first, with the cursor at column
// 1, then written whole: a terminal that draws a glyph narrower than Rex
// counts it (a ZWJ cluster, a text-style pictograph) can't leave stale
// cells past the row's end. There is deliberately no erase after the row:
// a write to the last column leaves the cursor on that column, and an
// erase there would wipe the row's last character.
func diffFrame(prev []string, next Frame, clear bool) string {
	var b strings.Builder
	b.WriteString(syncBegin)
	b.WriteString(HideCursor)
	if clear {
		b.WriteString(Home + Clear)
	}
	for i, row := range next.Rows {
		if prev != nil && i < len(prev) && prev[i] == row {
			continue
		}
		fmt.Fprintf(&b, "\033[%d;1H", i+1)
		b.WriteString(Reset + eraseLine)
		b.WriteString(row)
		b.WriteString(Reset)
	}
	if next.CursorRow > 0 {
		fmt.Fprintf(&b, "\033[%d;%dH%s", next.CursorRow, next.CursorCol, ShowCursor)
	}
	b.WriteString(syncEnd)
	return b.String()
}

// stripSGR removes color sequences, for measuring a painted row.
func stripSGR(s string) string {
	if !strings.Contains(s, "\033[") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
