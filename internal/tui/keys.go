package tui

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	escWait     = 50 * time.Millisecond  // a lone ESC is Esc only after this long
	pasteWait   = 200 * time.Millisecond // a stalled paste is flushed after this
	pasteGiveUp = 2 * time.Second        // and given up on after this
	pasteCap    = 64 << 10

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
	stalled bool // the paste went quiet once; what came so far was flushed
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
		if d.stalled {
			return true, pasteGiveUp
		}
		return true, pasteWait
	}
	return len(d.pending) > 0, escWait
}

// flush resolves whatever is still pending after its wait: a lone ESC is
// Esc, broken bytes are dropped, and a paste that went quiet is delivered
// so far but stays a paste (its newlines never submit) until its end
// marker comes or pasteGiveUp passes.
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
	if final && d.pasting && len(buf) == 0 {
		out = append(out, d.stallPaste()...)
	}
	i := 0
	for i < len(buf) {
		if d.pasting {
			end := strings.Index(string(buf[i:]), "\x1b[201~")
			if end < 0 {
				if final {
					d.paste = append(d.paste, buf[i:]...)
					out = append(out, d.stallPaste()...)
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
		if fb < 0x40 || fb > 0x7e {
			return "", j, true // not a sequence: drop ESC [ and its parameters
		}
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
				return "", 2, true // Alt+O
			}
			return "", 0, false
		}
		return csiFinal[b[2]], 3, true
	case 0x1b:
		// ESC ESC [ … is Alt plus a key sequence; drop it whole. Two lone
		// ESCs are two Escs.
		if len(b) == 2 && !final {
			return "", 0, false
		}
		if len(b) >= 3 && (b[2] == '[' || b[2] == 'O') {
			_, m, ok := d.escape(b[1:], final)
			if !ok {
				return "", 0, false
			}
			return "", 1 + m, true
		}
		return "esc", 1, true
	}
	// Alt+key: ESC then any other byte or one whole rune (Alt+Backspace,
	// Alt+Enter, Alt+é). Not a Rex key, and never Esc plus that key.
	if b[1] < 0x80 {
		return "", 2, true
	}
	if !utf8.FullRune(b[1:]) {
		if final {
			return "", len(b), true
		}
		return "", 0, false
	}
	_, size := utf8.DecodeRune(b[1:])
	return "", 1 + size, true
}

func firstParam(params string) string {
	if i := strings.IndexByte(params, ';'); i >= 0 {
		return params[:i]
	}
	return params
}

// endPaste turns the collected paste into one line: newlines and tabs
// become spaces and anything typing would refuse (controls, format
// characters) is dropped, so a paste can never submit, switch place, or
// inject a sequence.
// stallPaste runs when a paste goes quiet: the first time it hands over
// what came so far and keeps waiting for the rest as paste; the second
// time it gives up on the end marker.
func (d *keyDecoder) stallPaste() []string {
	if d.stalled {
		if tok := d.endPaste(); tok != PastePrefix {
			return []string{tok}
		}
		return nil
	}
	d.stalled = true
	if len(d.paste) == 0 {
		return nil
	}
	tok := PastePrefix + oneLine(strings.ToValidUTF8(string(d.paste), ""))
	d.paste = d.paste[:0]
	return []string{tok}
}

func (d *keyDecoder) endPaste() string {
	d.pasting, d.stalled = false, false
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
		case !unicode.IsPrint(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
