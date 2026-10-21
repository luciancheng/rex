package tui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	escWait   = 50 * time.Millisecond  // a lone ESC is Esc only after this long
	pasteWait = 200 * time.Millisecond // a paste with no end marker is flushed after this
	pasteCap  = 64 << 10

	// PastePrefix tags a bracketed paste. The rest of the token is the
	// pasted text on one line. A paste is inserted, never submitted.
	PastePrefix = "\x1b[200~"
)

// IsPaste reports whether key is a paste token, and returns its text.
func IsPaste(key string) (string, bool) {
	if !strings.HasPrefix(key, PastePrefix) {
		return "", false
	}
	return key[len(PastePrefix):], true
}

// keyDecoder turns terminal input bytes into key tokens. Bytes can arrive
// split anywhere (mid escape sequence, mid UTF-8 rune, mid paste), so
// whatever is incomplete waits in pending for the next read. Tokens are
// named keys ("up", "esc", "pageup"...), control bytes ("\r", "\t",
// "\x03"...), whole runes, or a PastePrefix token.
type keyDecoder struct {
	pending []byte
	last    time.Time // when the last bytes arrived
	paste   []byte
	pasting bool
}

var csiFinal = map[byte]string{
	'A': "up", 'B': "down", 'C': "right", 'D': "left", 'H': "home", 'F': "end",
}

var csiTilde = map[string]string{
	"1": "home", "7": "home", "4": "end", "8": "end", "5": "pageup", "6": "pagedown",
}

// feed decodes data and returns the complete tokens.
func (d *keyDecoder) feed(data []byte, now time.Time) []string {
	if len(data) == 0 {
		return nil
	}
	d.last = now
	d.pending = append(d.pending, data...)
	return d.decode(false)
}

// waiting reports whether bytes are held back and how long they may wait
// before flush gives up on them.
func (d *keyDecoder) waiting() (bool, time.Duration) {
	if d.pasting {
		return true, pasteWait
	}
	return len(d.pending) > 0, escWait
}

// flush resolves whatever is still pending after its wait: a lone ESC is
// Esc, an unfinished paste is delivered as is, and broken bytes are dropped.
func (d *keyDecoder) flush(now time.Time) []string {
	ok, wait := d.waiting()
	if !ok || now.Sub(d.last) < wait {
		return nil
	}
	return d.decode(true)
}

func (d *keyDecoder) decode(final bool) []string {
	var out []string
	buf := d.pending
	i := 0
	for i < len(buf) {
		if d.pasting {
			end := strings.Index(string(buf[i:]), "\x1b[201~")
			if end < 0 {
				if final {
					d.paste = append(d.paste, buf[i:]...)
					out = append(out, d.endPaste())
					break
				}
				// Keep a possible partial end marker for the next read.
				keep := len(buf) - i
				if keep > 5 {
					keep = 5
				}
				d.paste = append(d.paste, buf[i:len(buf)-keep]...)
				if len(d.paste) > pasteCap {
					d.paste = d.paste[:pasteCap]
				}
				i = len(buf) - keep
				goto wait
			}
			d.paste = append(d.paste, buf[i:i+end]...)
			i += end + len("\x1b[201~")
			out = append(out, d.endPaste())
			continue
		}
		c := buf[i]
		switch {
		case c == 0x1b:
			tok, n, ok := d.escape(buf[i:], final)
			if !ok {
				goto wait
			}
			if tok != "" {
				out = append(out, tok)
			}
			i += n
		case c == 0x01:
			out = append(out, "home")
			i++
		case c == 0x05:
			out = append(out, "end")
			i++
		case c < 0x80:
			out = append(out, string(c))
			i++
		default:
			r, size := utf8.DecodeRune(buf[i:])
			if r == utf8.RuneError && size <= 1 {
				if !final && !utf8.FullRune(buf[i:]) {
					goto wait
				}
				i++ // invalid byte: drop it
				continue
			}
			out = append(out, string(r))
			i += size
		}
	}
	d.pending = d.pending[:0]
	return out
wait:
	d.pending = append([]byte(nil), buf[i:]...)
	return out
}

// escape decodes one escape sequence at the start of b. ok is false when b
// is an incomplete sequence that more bytes may finish.
func (d *keyDecoder) escape(b []byte, final bool) (tok string, n int, ok bool) {
	if len(b) == 1 {
		if final {
			return "esc", 1, true
		}
		return "", 0, false
	}
	switch b[1] {
	case '[':
		j := 2
		for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
			j++
		}
		if j >= len(b) {
			if final {
				return "", len(b), true // broken sequence: drop it
			}
			return "", 0, false
		}
		fb := b[j]
		params := string(b[2:j])
		n = j + 1
		if fb == '~' {
			if params == "200" {
				d.pasting = true
				d.paste = d.paste[:0]
				return "", n, true
			}
			return csiTilde[firstParam(params)], n, true
		}
		if name, ok := csiFinal[fb]; ok {
			return name, n, true
		}
		return "", n, true // unknown CSI: drop, never type it
	case 'O':
		if len(b) < 3 {
			if final {
				return "esc", 1, true
			}
			return "", 0, false
		}
		return csiFinal[b[2]], 3, true
	case 0x1b:
		return "esc", 1, true
	}
	if b[1] >= 0x20 && b[1] < 0x7f {
		return "", 2, true // Alt+key: not a Rex key
	}
	return "esc", 1, true
}

func firstParam(params string) string {
	if i := strings.IndexByte(params, ';'); i >= 0 {
		return params[:i]
	}
	return params
}

// endPaste turns the collected paste into one line: newlines and tabs
// become spaces and other control characters are dropped, so a paste can
// never submit, switch place, or inject a sequence.
func (d *keyDecoder) endPaste() string {
	d.pasting = false
	if len(d.paste) > pasteCap {
		d.paste = d.paste[:pasteCap]
	}
	text := strings.ToValidUTF8(string(d.paste), "")
	d.paste = d.paste[:0]
	return PastePrefix + oneLine(text)
}

func oneLine(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\r' || r == '\n' || r == '\t':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
