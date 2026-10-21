package tui

import (
	"strings"
	"unicode/utf16"
)

// Windows console input, kept free of syscalls so it is tested on every OS.

const (
	consoleProcessedInput = 0x0001 // ENABLE_PROCESSED_INPUT
	consoleVTInput        = 0x0200 // ENABLE_VIRTUAL_TERMINAL_INPUT
)

// rawInputMode turns off processed input so Ctrl+C reaches Rex as a key
// (and "ctrl+c twice" works) instead of killing the process before the
// screen is restored, and VT input, which would hand Rex escape sequences
// as characters. Everything else in the mode is kept.
func rawInputMode(orig uint32) uint32 {
	return orig &^ (consoleProcessedInput | consoleVTInput)
}

type consoleKey struct {
	Down bool
	VK   uint16
	Char uint16
}

// consoleKeys turns key records into tokens, joining UTF-16 surrogate
// halves into one rune.
type consoleKeys struct {
	high uint16
}

func (d *consoleKeys) token(k consoleKey) string {
	if !k.Down {
		return ""
	}
	if k.VK == 0x1B || k.Char == 0x1B {
		return "esc"
	}
	if name := virtualKeyName(k.VK); name != "" && k.Char == 0 {
		return name
	}
	c := k.Char
	switch {
	case c == 0:
		return ""
	case c >= 0xD800 && c < 0xDC00:
		d.high = c
		return ""
	case c >= 0xDC00 && c < 0xE000:
		high := d.high
		d.high = 0
		if high == 0 {
			return ""
		}
		return string(utf16.DecodeRune(rune(high), rune(c)))
	}
	d.high = 0
	switch c {
	case 1:
		return "home"
	case 5:
		return "end"
	}
	return string(rune(c))
}

func virtualKeyName(vk uint16) string {
	switch vk {
	case 0x26:
		return "up"
	case 0x28:
		return "down"
	case 0x25:
		return "left"
	case 0x27:
		return "right"
	case 0x21:
		return "pageup"
	case 0x22:
		return "pagedown"
	case 0x24:
		return "home"
	case 0x23:
		return "end"
	default:
		return ""
	}
}

// coalescePaste treats a burst of characters with a newline inside it as a
// paste: the Windows console delivers a paste as single key events, and
// an embedded Enter must not submit. A short burst that only ends in Enter
// is typing that the console queued up (a letter and ⏎ read together), so
// it stays keys. Best effort: a pasted single short line ending in a
// newline still reads as typed.
func coalescePaste(tokens []string) []string {
	if len(tokens) < 2 {
		return tokens
	}
	inside := false // text after a newline
	newline := false
	for _, t := range tokens {
		switch {
		case t == "\r" || t == "\n":
			newline = true
		case t == "\t" || (len([]rune(t)) == 1 && t >= " "):
			if newline {
				inside = true
			}
		default:
			return tokens
		}
	}
	if !newline || (!inside && len(tokens) < minPasteBurst) {
		return tokens
	}
	text := strings.Join(tokens, "")
	if len(text) > pasteCap {
		text = text[:pasteCap]
	}
	return []string{PastePrefix + oneLine(strings.ToValidUTF8(text, ""))}
}

// minPasteBurst is how many characters at once can't be typing.
const minPasteBurst = 16
