package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func TestDiffFrameRepaintsOnlyChangedRows(t *testing.T) {
	prev := []string{"aaaa", "bbbb", "cccc"}
	out := diffFrame(prev, Frame{Rows: []string{"aaaa", "BBBB", "cccc"}, CursorRow: 3, CursorCol: 2}, false)
	if strings.Contains(out, Clear) || strings.Contains(out, "aaaa") || strings.Contains(out, "cccc") {
		t.Fatalf("unchanged rows repainted: %q", out)
	}
	if !strings.Contains(out, "\033[2;1H"+Reset+eraseLine+"BBBB"+Reset) || strings.Contains(out, "\033[K") {
		t.Fatalf("a changed row is erased from column 1, then written whole, never erased after: %q", out)
	}
	if !strings.HasPrefix(out, syncBegin+HideCursor) || !strings.HasSuffix(out, "\033[3;2H"+ShowCursor+syncEnd) {
		t.Fatalf("framing %q", out)
	}
}

func TestDiffFrameHidesCursorWhenNone(t *testing.T) {
	out := diffFrame(nil, Frame{Rows: []string{"x"}}, true)
	if strings.Contains(out, ShowCursor) || !strings.Contains(out, Home+Clear) {
		t.Fatalf("%q", out)
	}
}

func TestRendererFullRepaintRules(t *testing.T) {
	r := &renderer{full: true}
	first := r.render(Frame{Rows: []string{"ab", "cd"}})
	if !strings.Contains(first, Clear) {
		t.Fatal("first frame clears")
	}
	same := r.render(Frame{Rows: []string{"ab", "cd"}})
	if strings.Contains(same, "ab") || strings.Contains(same, Clear) {
		t.Fatalf("identical frame should paint nothing: %q", same)
	}
	if out := r.render(Frame{Rows: []string{"ab", "cd", "ef"}}); !strings.Contains(out, Clear) {
		t.Fatal("height change repaints fully")
	}
	if out := r.render(Frame{Rows: []string{"abc", "cde", "efg"}}); !strings.Contains(out, Clear) {
		t.Fatal("width change repaints fully")
	}
	r.invalidate()
	if out := r.render(Frame{Rows: []string{"abc", "cde", "efg"}}); !strings.Contains(out, Clear) {
		t.Fatal("invalidate repaints fully")
	}
}

func TestComposeFrameStringMatchesRows(t *testing.T) {
	opt := ComposeOptions{Enabled: false, Columns: 100, Lines: 24, Pane: PaneChat,
		Log: []watch.Event{watch.NewEvent("cursor", "text", "hello", nil)}}
	f := composeRows("idle", "yellow", opt)
	if got := ComposeFrame("idle", "yellow", opt); !strings.Contains(got, strings.Join(f.Rows, "\r\n")) {
		t.Fatal("string frame and rows disagree")
	}
	if len(f.Rows) != 24 || f.CursorRow != 22 {
		t.Fatalf("rows=%d cursor=%d", len(f.Rows), f.CursorRow)
	}
}

func TestLogViewIsCachedPerRevision(t *testing.T) {
	a := testApp(t)
	a.ingest([]watch.Event{watch.NewEvent("cursor", "text", "one", nil)})
	first := a.logView(60)
	again := a.logView(60)
	if &first[0] != &again[0] {
		t.Fatal("same revision and width should reuse the layout")
	}
	a.ingest([]watch.Event{watch.NewEvent("cursor", "text", "two", nil)})
	if strings.Contains(viewText(first), "two") || !strings.Contains(viewText(a.logView(60)), "two") {
		t.Fatal("a new event must rebuild the layout")
	}
}

// The caches never show stale content: a note typed after the tape was
// laid out appears, a trim-only ingest relays the tape, and the mascot is
// cached per color mode.
func TestCachesRefresh(t *testing.T) {
	a := testApp(t)
	press(a, "\t")
	a.frame() // fills the tape cache
	typeText(a, "a fresh note")
	press(a, "\r")
	if !strings.Contains(a.frame(), "a fresh note") {
		t.Fatal("a note typed after the cache filled must show")
	}
	var evs []watch.Event
	for i := 0; i < MaxLog; i++ {
		evs = append(evs, watch.NewEvent("cursor", "text", fmt.Sprintf("reply %d", i), nil))
	}
	a.ingest(evs)
	rev := a.logRev
	n := len(a.log)
	a.ingest([]watch.Event{watch.NewEvent("cursor", "text", "one more", nil)})
	if len(a.log) != n || a.logRev == rev {
		t.Fatalf("a trim to MaxLog relays the tape: len %d->%d rev %d->%d", n, len(a.log), rev, a.logRev)
	}
	colored := stagedMascot("idle", "yellow", 40, true)
	plain := stagedMascot("idle", "yellow", 40, false)
	if !strings.Contains(strings.Join(colored, ""), "\033[") || strings.Contains(strings.Join(plain, ""), "\033[") {
		t.Fatal("the mascot cache is keyed on color")
	}
}
