package watch

import (
	"strings"
	"unicode/utf8"
)

const tabWidth = 4

// CleanText makes agent or plan text safe to paint: invalid UTF-8 and
// control characters are dropped (only "\n" survives), escape and OSC
// sequences are removed so text can never recolor the screen, retitle the
// window or write the clipboard, bidi overrides are removed so a line can't
// visually reorder itself, "\r" is dropped, and tabs become spaces.
func CleanText(s string) string {
	if isPlain(s) {
		return s
	}
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b:
			i += escapeLen(s[i:])
			continue
		case r == '\n':
			b.WriteByte('\n')
			col = 0
		case r == '\t':
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0):
		case isBidiControl(r):
		default:
			b.WriteRune(r)
			col++
		}
		i += size
	}
	return b.String()
}

func isPlain(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 && c != '\n' || c == 0x7f || c >= 0x80 {
			return false
		}
	}
	return true
}

func isBidiControl(r rune) bool {
	return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f || r == 0x061c
}

// escapeLen is the length of the escape sequence at the start of s (which
// begins with ESC): CSI up to its final byte, OSC/DCS/APC/PM up to BEL or
// ST, or ESC plus one byte.
func escapeLen(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		for j := 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', 'P', '_', '^', 'X':
		for j := 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return 2
}
