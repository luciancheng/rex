//go:build unix

package tui

import (
	"os"
	"reflect"
	"testing"
	"time"
)

// When the terminal goes away with bytes still pending (a lone ESC, a
// paste start), Read resolves them and returns instead of spinning, so the
// live loop gets back to its signal checks.
func TestKeyReaderReturnsAtEOF(t *testing.T) {
	for _, pending := range []string{"\x1b", "\x1b[200~half a paste"} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.WriteString(pending)
		w.Close()
		k := newKeyReader(r)
		done := make(chan []string, 1)
		go func() { done <- k.Read(20 * time.Millisecond) }()
		select {
		case got := <-done:
			if pending == "\x1b" && !reflect.DeepEqual(got, []string{"esc"}) {
				t.Errorf("%q: %q", pending, got)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%q: Read spun at EOF", pending)
		}
		r.Close()
	}
}
