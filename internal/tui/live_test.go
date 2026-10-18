package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luciancheng/rex/internal/mascot"
	"github.com/luciancheng/rex/internal/watch"
)

func TestMarkdownUserPromptAlignment(t *testing.T) {
	view := BuildLogView([]watch.Event{{Source: "cursor", Kind: "user", Text: "format this please"}}, 36)
	if view[0].Kind != "user" {
		t.Fatalf("kind %s", view[0].Kind)
	}
	for _, line := range view {
		if strings.Contains(line.Text, "──") || strings.TrimSpace(strings.ToLower(line.Text)) == "you" {
			t.Fatalf("unexpected label %q", line.Text)
		}
	}
	var body []string
	for _, line := range view {
		if line.Kind == "user" && strings.Contains(line.Text, "format this please") {
			body = append(body, line.Text)
		}
	}
	if len(body) == 0 {
		t.Fatal("missing body")
	}
	if !strings.HasSuffix(strings.TrimRight(body[0], " "), "format this please") {
		t.Fatalf("%q", body[0])
	}
	if !strings.HasPrefix(body[0], " ") {
		t.Fatal("not right aligned")
	}
}

func TestUserPromptBoxIsLeftAlignedInside(t *testing.T) {
	prompt := "one two three four five six seven eight nine ten eleven"
	width := 40
	view := BuildLogView([]watch.Event{{Source: "cursor", Kind: "user", Text: prompt}}, width)
	var body []string
	for _, line := range view {
		if line.Kind == "user" && strings.TrimSpace(line.Text) != "" {
			body = append(body, line.Text)
		}
	}
	if len(body) <= 1 {
		t.Fatalf("expected wrap, got %#v", body)
	}
	half := width / 2
	starts := map[int]bool{}
	var start int
	for _, line := range body {
		n := len(line) - len(strings.TrimLeft(line, " "))
		starts[n] = true
		start = n
		if line[start] == ' ' {
			t.Fatalf("interior not left aligned %q", line)
		}
		trimmed := strings.TrimRight(line, " ")
		if len([]rune(trimmed))-start > half {
			t.Fatalf("wider than half %q", line)
		}
		if len([]rune(strings.TrimRight(line, " "))) > width {
			t.Fatalf("wider than pane")
		}
	}
	if len(starts) != 1 {
		t.Fatalf("ragged box %v", starts)
	}
	if start < width-half {
		t.Fatalf("not right aligned start=%d", start)
	}
}

func TestComposerAnimationBreakdance(t *testing.T) {
	if ComposerAnimation("breakdance", "idle") != "breakdance" {
		t.Fatal("expected breakdance")
	}
	if ComposerAnimation(" BreakDance ", "idle") != "breakdance" {
		t.Fatal("case/space")
	}
	if ComposerAnimation("idle", "breakdance") != "idle" {
		t.Fatal("idle restore")
	}
	if ComposerAnimation("hello", "idle") != "idle" {
		t.Fatal("plain text should keep current")
	}
}

func TestAdvanceBreakdancePlaysOnce(t *testing.T) {
	frames := mascot.MustAnimation("breakdance").Frames
	index, hold := 0, frames[0].Hold
	steps := 0
	for {
		var done bool
		index, hold, done = advance(frames, index, hold, false)
		steps++
		if done {
			break
		}
		if steps > 10000 {
			t.Fatal("did not finish")
		}
	}
	if index != len(frames)-1 {
		t.Fatalf("ended on %d, want last frame", index)
	}
	_, _, looped := advance(frames, len(frames)-1, 1, true)
	if looped {
		t.Fatal("idle-style loop should wrap, not finish")
	}
}

func TestExtendLogDropsRepeatedThoughts(t *testing.T) {
	var log []watch.Event
	first := watch.NewEvent("cursor", "thought", "Looking at live.py", nil)
	added := ExtendLog(&log, []watch.Event{first, watch.NewEvent("cursor", "thought", "Looking at live.py", nil)})
	if added != 1 || len(log) != 1 {
		t.Fatalf("added=%d len=%d", added, len(log))
	}
}

func TestPrintCardIncludesIdentity(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintCard("idle", "yellow", false, &buf); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	if !strings.Contains(text, "REX") || !strings.Contains(text, "idle") {
		t.Fatalf("%s", text)
	}
}

func TestNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("FORCE_COLOR", "")
	if mascot.ColorEnabledDefault() {
		t.Fatal("NO_COLOR should disable color")
	}
}

func TestUnknownPalette(t *testing.T) {
	_, err := mascot.RenderSprite(mascot.MustSprite("idle"), "sparkle", true)
	if err == nil || !strings.Contains(err.Error(), "unknown color") {
		t.Fatalf("%v", err)
	}
}

func TestNoColorHasNoANSI(t *testing.T) {
	out, err := mascot.RenderSprite(mascot.MustSprite("idle"), "yellow", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\033") {
		t.Fatal("ansi in no-color")
	}
	if len(strings.Split(out, "\n")) <= 4 {
		t.Fatal("too short")
	}
}

func TestColorUsesTruecolor(t *testing.T) {
	out, err := mascot.RenderSprite(mascot.MustSprite("idle"), "yellow", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\033[38;2;") {
		t.Fatal("missing truecolor")
	}
}

func TestIdlePNGMatchesSprite(t *testing.T) {
	root := findRoot(t)
	image, err := mascot.LoadPNG(root + "/assets/idle.png")
	if err != nil {
		t.Fatal(err)
	}
	if image.Width != 40 || image.Height != 40 {
		t.Fatalf("%dx%d", image.Width, image.Height)
	}
	palette := mascot.Palettes["yellow"]
	if len(mascot.IdleRows) != image.Height || len(mascot.IdleRows[0]) != image.Width {
		t.Fatal("size mismatch")
	}
	for y, row := range mascot.IdleRows {
		for x, ch := range row {
			pix := image.Pixels[y][x]
			r, g, b, a := pix[0], pix[1], pix[2], pix[3]
			if ch == '.' {
				if !((r == 255 && g == 255 && b == 255) || a < 16) {
					t.Fatalf("transparent mismatch %d,%d", x, y)
				}
				continue
			}
			role := mascot.PixelRoles[ch]
			want := palette[role]
			if want != (mascot.RGB{r, g, b}) {
				t.Fatalf("%d,%d %c %v != %v", x, y, ch, want, mascot.RGB{r, g, b})
			}
		}
	}
}

func findRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("no go.mod")
	return ""
}
