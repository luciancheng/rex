package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/watch"
)

func TestCellWidth(t *testing.T) {
	for r, want := range map[rune]int{
		'a': 1, 'é': 1, '中': 2, '한': 2, 'Ａ': 2, '😀': 2, '🚀': 2,
		'\u0301': 0, '\u200d': 0, '\ufe0f': 0,
		'·': 1, '…': 1, '─': 1, '│': 1, '▌': 1, '○': 1, '✓': 1, '▸': 1, '↑': 1,
		'•': 1, '›': 1, '×': 1, '✗': 1, '━': 1, '→': 1, // the redesign's glyphs (docs/ux.md §5)
	} {
		if got := cellWidth(r); got != want {
			t.Errorf("cellWidth(%q) = %d, want %d", r, got, want)
		}
	}
}

func TestClipByCells(t *testing.T) {
	if got := clip("中文标题很长", 7); got != "中文标…" || cellLen(got) != 7 {
		t.Fatalf("%q (%d cells)", got, cellLen(got))
	}
	if got := clip("中文", 5); got != "中文 " {
		t.Fatalf("pad %q", got)
	}
	if got := clip("abcdef", 4); got != "abc…" {
		t.Fatalf("%q", got)
	}
	if got := clip("ab", 4); got != "ab  " {
		t.Fatalf("%q", got)
	}
	if got := clip("e\u0301te", 3); got != "e\u0301te" {
		t.Fatalf("combining mark takes no cell: %q", got)
	}
	if got := clip("line\nbreak", 20); strings.Contains(got, "\n") {
		t.Fatalf("clip must keep one row: %q", got)
	}
}

func TestWrapByCells(t *testing.T) {
	for _, row := range wrap("中文 中文中文 中文中文中文中文中文", 8) {
		if cellLen(row) > 8 {
			t.Fatalf("row %q is %d cells", row, cellLen(row))
		}
	}
	for _, row := range hardWrap("中文中文中", 3) {
		if cellLen(row) > 3 || row == "" {
			t.Fatalf("hard row %q", row)
		}
	}
}

// frameRows strips only the framing Rex writes around a NO_COLOR frame,
// where Rex writes it: Home+Clear before the rows, the cursor move and
// show/hide after them. What is left are the rows, which in a NO_COLOR
// frame carry no escapes at all.
var frameTail = regexp.MustCompile(`(\x1b\[\d+;\d+H)?\x1b\[\?25[hl]$`)

func frameRows(t *testing.T, frame string) []string {
	t.Helper()
	if !strings.HasPrefix(frame, Home+Clear) || !frameTail.MatchString(frame) {
		t.Fatalf("not a frame: %q", frame)
	}
	body := frameTail.ReplaceAllString(strings.TrimPrefix(frame, Home+Clear), "")
	return strings.Split(body, "\r\n")
}

func assertNoForeignControl(t *testing.T, frame string) {
	t.Helper()
	for _, row := range frameRows(t, frame) {
		for _, r := range row {
			if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
				t.Fatalf("control %U reached the terminal:\n%q", r, row)
			}
		}
	}
}

func TestHostileAgentTextNeverReachesTheTerminal(t *testing.T) {
	hostile := []watch.Event{
		{Source: "cursor", Kind: "thought", Text: "steal \x1b]52;c;ZXZpbA==\x07 the clipboard"},
		{Source: "cursor", Kind: "text", Text: "## Done\n```\n\tcode\x1b[2J\n```\nleave \x1b[?1049l alt"},
		{Source: "cursor", Kind: "user", Text: "prompt\rover \u202eevil"},
		{Source: "cursor", Kind: "command", Text: "printf '\x1b]0;title\x07'", Meta: map[string]any{"tool": "shell"}},
		{Source: "cursor", Kind: "error", Text: "\x1b[31mFAIL\x1b[0m 中文 😀"},
	}
	for _, width := range []int{80, 120} {
		frame := ComposeFrame("idle", "yellow", ComposeOptions{
			Enabled: false, Columns: width, Lines: 30,
			Log: hostile, Pane: PaneChat, Status: "idle\x1b[2J", PlanRel: "rex/plans/\x1b]52;c;eA==\x07x.rex.json",
			Draft: "draft \x1b[2J",
		})
		assertNoForeignControl(t, frame)
		for _, row := range frameRows(t, frame) {
			if cellLen(row) > width {
				t.Fatalf("row wider than %d: %q", width, row)
			}
		}
	}
}

func TestHostilePlanFileNamesAreCleaned(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "rex", "plans")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "evil\x1b]52;c;eA==\x07name.rex.json"
	if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
		t.Skipf("filesystem refuses the name: %v", err)
	}
	b := NewPlanBrowser(repo)
	frame := ComposeFrame("idle", "yellow", ComposeOptions{Enabled: false, Columns: 120, Lines: 30, Pane: PanePlans, Plans: b, Composer: true})
	assertNoForeignControl(t, frame)
	if !strings.Contains(frame, "evilname.rex.json") {
		t.Fatal("cleaned name should still show")
	}
}

func TestEmojiSequencesMeasureAsDrawn(t *testing.T) {
	for _, c := range []struct {
		s string
		n int
	}{
		{"⚠️", 2}, {"✔️", 2}, {"1️⃣", 2}, {"👍🏽", 2}, {"👨‍💻", 2}, {"a⚠️b", 4}, {"⚠", 1},
	} {
		if got := cellLen(c.s); got != c.n {
			t.Errorf("cellLen(%q) = %d, want %d", c.s, got, c.n)
		}
	}
	if got := clip("a⚠️b", 3); got != "a⚠…" {
		t.Fatalf("%q", got)
	}
	if got := tailCells("x👨‍💻", 1); got != "" {
		t.Fatalf("a tail never starts inside a sequence: %q", got)
	}
	if got := tailCells("x👨‍💻", 2); got != "👨‍💻" {
		t.Fatalf("%q", got)
	}
}

// A ZWJ joins pictographs only: between letters or CJK it is just
// zero-width, and what follows counts in full. A tail never starts on a
// modifier.
func TestZWJJoinsOnlyPictographs(t *testing.T) {
	if cellLen("a\u200db") != 2 || cellLen(strings.Repeat("\u200d中", 4)) != 8 {
		t.Fatalf("a\u200db %d, \u200d中×4 %d", cellLen("a\u200db"), cellLen(strings.Repeat("\u200d中", 4)))
	}
	if got := clip(strings.Repeat("\u200d中", 4), 5); cellLen(got) != 5 || !strings.Contains(got, "…") {
		t.Fatalf("%q", got)
	}
	for _, s := range []string{"ab⚠\ufe0f", "x1\ufe0f\u20e3"} {
		if got := tailCells(s, 1); strings.ContainsAny(got, "\ufe0f\u20e3") {
			t.Errorf("tailCells(%q, 1) starts on a modifier: %q", s, got)
		}
	}
}
