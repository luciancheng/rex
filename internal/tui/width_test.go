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

// rexSequences are the escapes Rex itself writes in a NO_COLOR frame.
var rexSequences = regexp.MustCompile(`\x1b\[(\?(25|7|1049|2004)[hl]|2J|H|\d+;\d+H)`)

func assertNoForeignControl(t *testing.T, frame string) {
	t.Helper()
	rest := rexSequences.ReplaceAllString(frame, "")
	rest = strings.ReplaceAll(rest, "\r\n", "")
	for _, r := range rest {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			t.Fatalf("control %U reached the terminal:\n%q", r, rest)
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
			Enabled: false, Columns: width, Lines: 30, AnimationName: "idle",
			Log: hostile, Pane: PaneChat, Status: "idle · rex/plans/\x1b]52;c;eA==\x07x.rex.json",
			Draft: "draft \x1b[2J",
		})
		assertNoForeignControl(t, frame)
		for _, row := range strings.Split(frame, "\r\n") {
			if cellLen(rexSequences.ReplaceAllString(row, "")) > width {
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
	frame := ComposeFrame("idle", "yellow", ComposeOptions{Enabled: false, Columns: 120, Lines: 30, AnimationName: "idle", Pane: PanePlans, Plans: b})
	assertNoForeignControl(t, frame)
	if !strings.Contains(frame, "evilname.rex.json") {
		t.Fatal("cleaned name should still show")
	}
}
