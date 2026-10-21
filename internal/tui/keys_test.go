package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func decodeAll(t *testing.T, chunks ...string) []string {
	t.Helper()
	var d keyDecoder
	now := time.Unix(0, 0)
	var out []string
	for _, c := range chunks {
		out = append(out, d.feed([]byte(c), now)...)
		now = now.Add(time.Millisecond)
	}
	return append(out, d.flush(now.Add(time.Second))...)
}

func TestDecoderSplitsQueuedArrows(t *testing.T) {
	got := decodeAll(t, "\x1b[A\x1b[A\x1b[B\x1bOA\x1b[5~\x1b[6~\x1b[H\x1b[4~")
	want := []string{"up", "up", "down", "up", "pageup", "pagedown", "home", "end"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestDecoderModifiedArrowsAndUnknownCSI(t *testing.T) {
	got := decodeAll(t, "\x1b[1;5A\x1b[1;2Dx\x1b[38;5;1mz\x1b[3~")
	want := []string{"up", "left", "x", "z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unknown sequences must be dropped, not typed: %q", got)
	}
}

func TestDecoderSequenceSplitAcrossReads(t *testing.T) {
	got := decodeAll(t, "\x1b", "[", "A", "\x1b[", "6", "~")
	if !reflect.DeepEqual(got, []string{"up", "pagedown"}) {
		t.Fatalf("%q", got)
	}
}

func TestDecoderLoneEscIsEscAfterWait(t *testing.T) {
	var d keyDecoder
	now := time.Unix(0, 0)
	if got := d.feed([]byte{0x1b}, now); len(got) != 0 {
		t.Fatalf("esc must wait for a possible sequence: %q", got)
	}
	if got := d.flush(now.Add(10 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("too early: %q", got)
	}
	if got := d.flush(now.Add(escWait)); !reflect.DeepEqual(got, []string{"esc"}) {
		t.Fatalf("%q", got)
	}
	if got := decodeAll(t, "\x1b\x1b"); !reflect.DeepEqual(got, []string{"esc", "esc"}) {
		t.Fatalf("%q", got)
	}
}

func TestDecoderAltChordIsIgnored(t *testing.T) {
	if got := decodeAll(t, "\x1bpz"); !reflect.DeepEqual(got, []string{"z"}) {
		t.Fatalf("%q", got)
	}
}

func TestDecoderUTF8SplitAcrossReads(t *testing.T) {
	got := decodeAll(t, "caf\xc3", "\xa9 \xe4\xb8", "\xad")
	if strings.Join(got, "") != "café 中" || len(got) != 6 {
		t.Fatalf("%q", got)
	}
	if got := decodeAll(t, "a\xffb"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("invalid bytes dropped: %q", got)
	}
}

func TestDecoderControlsAndCtrlKeys(t *testing.T) {
	got := decodeAll(t, "\x03\t\r\x7f\x01\x05\x15\x17")
	want := []string{"\x03", "\t", "\r", "\x7f", "home", "end", "\x15", "\x17"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestDecoderBracketedPasteIsOneToken(t *testing.T) {
	got := decodeAll(t, "a\x1b[200~line one\r\nline\ttwo\x1b[31mred", "\x1b[20", "1~b")
	if len(got) != 3 || got[0] != "a" || got[2] != "b" {
		t.Fatalf("%q", got)
	}
	text, ok := IsPaste(got[1])
	if !ok || text != "line one  line two[31mred" {
		t.Fatalf("paste %q ok=%v", text, ok)
	}
	if strings.ContainsAny(text, "\r\n\t\x1b") {
		t.Fatal("paste must not carry controls")
	}
}

func TestDecoderPasteWithoutEndMarkerIsFlushed(t *testing.T) {
	var d keyDecoder
	now := time.Unix(0, 0)
	d.feed([]byte("\x1b[200~unterminated"), now)
	if got := d.flush(now.Add(50 * time.Millisecond)); len(got) != 0 {
		t.Fatalf("too early %q", got)
	}
	got := d.flush(now.Add(pasteWait))
	if len(got) != 1 {
		t.Fatalf("%q", got)
	}
	if text, _ := IsPaste(got[0]); text != "unterminated" {
		t.Fatalf("%q", text)
	}
}

func TestDecoderPasteIsCapped(t *testing.T) {
	big := strings.Repeat("x", pasteCap*2)
	got := decodeAll(t, "\x1b[200~"+big[:pasteCap], big[pasteCap:]+"\x1b[201~")
	if len(got) != 1 {
		t.Fatalf("%d tokens", len(got))
	}
	if text, _ := IsPaste(got[0]); len(text) > pasteCap+8 {
		t.Fatalf("paste not capped: %d", len(text))
	}
}

func TestConsoleKeys(t *testing.T) {
	var d consoleKeys
	cases := []struct {
		k    consoleKey
		want string
	}{
		{consoleKey{Down: true, VK: 0x1B, Char: 0x1B}, "esc"},
		{consoleKey{Down: true, VK: 0x43, Char: 3}, "\x03"},
		{consoleKey{Down: true, VK: 0x26}, "up"},
		{consoleKey{Down: true, VK: 0x21}, "pageup"},
		{consoleKey{Down: true, VK: 0x41, Char: 'a'}, "a"},
		{consoleKey{Down: false, VK: 0x41, Char: 'a'}, ""},
		{consoleKey{Down: true, VK: 0x41, Char: 1}, "home"},
	}
	for _, c := range cases {
		if got := d.token(c.k); got != c.want {
			t.Fatalf("%+v: %q want %q", c.k, got, c.want)
		}
	}
	if d.token(consoleKey{Down: true, Char: 0xD83D}) != "" {
		t.Fatal("high surrogate waits")
	}
	if got := d.token(consoleKey{Down: true, Char: 0xDE00}); got != "😀" {
		t.Fatalf("surrogate pair %q", got)
	}
	if got := d.token(consoleKey{Down: true, Char: 0xDE00}); got != "" {
		t.Fatalf("lone low surrogate %q", got)
	}
}

func TestRawInputModeOnlyClearsProcessedInput(t *testing.T) {
	const orig = 0x01 | 0x02 | 0x04 | 0x40 | 0x80
	if got := rawInputMode(orig); got != orig&^0x01 {
		t.Fatalf("%#x", got)
	}
}

func TestCoalescePaste(t *testing.T) {
	if got := coalescePaste([]string{"h", "i", "\r", "y", "o"}); len(got) != 1 {
		t.Fatalf("burst with newline should be one paste: %q", got)
	} else if text, _ := IsPaste(got[0]); text != "hi yo" {
		t.Fatalf("%q", text)
	}
	if got := coalescePaste([]string{"a", "b"}); len(got) != 2 {
		t.Fatal("plain typing stays keys")
	}
	if got := coalescePaste([]string{"a", "up", "\r"}); len(got) != 3 {
		t.Fatal("named keys are not a paste")
	}
}

func TestPasteGoesIntoTheDraftAndNeverSubmits(t *testing.T) {
	a := testApp(t)
	press(a, "\t", PastePrefix+"line one  line two")
	if a.draft != "line one  line two" || len(a.log) != 0 {
		t.Fatalf("draft %q log %d", a.draft, len(a.log))
	}
	press(a, PastePrefix+strings.Repeat("é", 400))
	if n := runeLen(a.draft); n != draftCap {
		t.Fatalf("draft capped by runes: %d", n)
	}
}
