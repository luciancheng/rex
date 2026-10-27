package mascot

import (
	"strings"
	"testing"
)

func TestIdleIsRegistered(t *testing.T) {
	sprite := MustSprite("idle")
	if sprite.Name != "idle" {
		t.Fatalf("%s", sprite.Name)
	}
	width, height, err := SpriteSize(sprite.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if width != sprite.Width() || width != StageSize {
		t.Fatalf("width %d", width)
	}
	if height != len(sprite.Rows) || height != StageSize {
		t.Fatalf("height %d", height)
	}
}

func TestIdleHasDinoFeatures(t *testing.T) {
	pixels := strings.Join(MustSprite("idle").Rows, "")
	for _, ch := range []string{"k", "y", "b", "d", "s"} {
		if !strings.Contains(pixels, ch) {
			t.Fatalf("missing %s", ch)
		}
	}
}

func TestIdleAnimationHasLife(t *testing.T) {
	animation := MustAnimation(DefaultAnimation)
	names := map[string]bool{}
	for _, frame := range animation.Frames {
		names[frame.Sprite] = true
	}
	for _, want := range []string{"idle", "idle_blink", "idle_look", "idle_breathe"} {
		if !names[want] {
			t.Fatalf("missing frame %s", want)
		}
	}
	if len(names) != 4 {
		t.Fatalf("%v", names)
	}
	openEye := MustSprite("idle").Rows
	closed := MustSprite("idle_blink").Rows
	looking := MustSprite("idle_look").Rows
	if openEye[13][25] != 'k' {
		t.Fatal("open eye")
	}
	if closed[13][25] != 'y' || closed[14][25] != 'k' {
		t.Fatal("blink")
	}
	if looking[13][26] != 'k' || looking[14][26] != 'k' {
		t.Fatal("look")
	}
	breathe := MustSprite("idle_breathe").Rows
	if breathe[0] != strings.Repeat(".", 40) {
		t.Fatal("breathe top")
	}
	if breathe[len(breathe)-1] != openEye[len(openEye)-1] {
		t.Fatal("shadow moved")
	}
	if breathe[14][25] != openEye[13][25] {
		t.Fatal("squat eye")
	}
}

func TestFitStageReserves40(t *testing.T) {
	idle := MustSprite("idle").Rows
	staged := FitStageDefault(idle)
	w, h, err := SpriteSize(staged)
	if err != nil {
		t.Fatal(err)
	}
	if w != StageSize || h != StageSize {
		t.Fatalf("%dx%d", w, h)
	}
	if strings.Join(staged, "\n") != strings.Join(idle, "\n") {
		t.Fatal("idle should already fill the stage")
	}
}

func TestFitStageKeepsTopLeftWhenClipping(t *testing.T) {
	rows := []string{"abcdefghij", "klmnopqrst"}
	clipped := FitStage(rows, 4, 1)
	if len(clipped) != 1 || clipped[0] != "abcd" {
		t.Fatalf("%v", clipped)
	}
}

func TestUnknownSprite(t *testing.T) {
	_, err := GetSprite("not-a-sprite")
	if err == nil || !strings.Contains(err.Error(), "unknown sprite") {
		t.Fatalf("%v", err)
	}
}

func TestRegisterSpriteAndAnimation(t *testing.T) {
	extra := RegisterSprite(Sprite{Name: "dot", Rows: []string{".e."}})
	defer delete(Sprites, "dot")
	defer delete(Animations, "blink")
	got, err := GetSprite("dot")
	if err != nil || got.Name != extra.Name {
		t.Fatal(err)
	}
	anim := RegisterAnimation(Animation{Name: "blink", Frames: []Frame{{"idle", 2}, {"dot", 1}}})
	if Animations["blink"].Name != anim.Name {
		t.Fatal("animation not registered")
	}
}

func TestBreakdanceSheetIsRegistered(t *testing.T) {
	anim, err := GetAnimation("breakdance")
	if err != nil {
		t.Fatal(err)
	}
	if len(anim.Frames) < 8 {
		t.Fatalf("too few frames: %d", len(anim.Frames))
	}
	for _, frame := range anim.Frames {
		sprite := MustSprite(frame.Sprite)
		w, h, err := SpriteSize(sprite.Rows)
		if err != nil {
			t.Fatal(err)
		}
		if w != StageSize || h != StageSize {
			t.Fatalf("%s is %dx%d", sprite.Name, w, h)
		}
		if frame.Hold != BreakdanceHold {
			t.Fatalf("hold %d", frame.Hold)
		}
	}
	pixels := strings.Join(MustSprite(anim.Frames[0].Sprite).Rows, "")
	if !strings.Contains(pixels, "y") || !strings.Contains(pixels, "k") {
		t.Fatal("first dance frame missing body/outline")
	}
}

func TestTypingSheetIsRegistered(t *testing.T) {
	anim, err := GetAnimation("typing")
	if err != nil {
		t.Fatal(err)
	}
	if len(anim.Frames) < 8 {
		t.Fatalf("too few frames: %d", len(anim.Frames))
	}
	for _, frame := range anim.Frames {
		sprite := MustSprite(frame.Sprite)
		w, h, err := SpriteSize(sprite.Rows)
		if err != nil {
			t.Fatal(err)
		}
		if w != StageSize || h != StageSize {
			t.Fatalf("%s is %dx%d", sprite.Name, w, h)
		}
	}
	pixels := strings.Join(MustSprite(anim.Frames[0].Sprite).Rows, "")
	if !strings.Contains(pixels, "y") || !strings.Contains(pixels, "k") {
		t.Fatal("first typing frame missing body/outline")
	}
}

func TestPalettesDefineEveryRole(t *testing.T) {
	for name, palette := range Palettes {
		for _, role := range PixelRoles {
			if _, ok := palette[role]; !ok {
				t.Fatalf("%s missing %s", name, role)
			}
		}
	}
}

func TestDetectDepth(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	cases := []struct {
		kv   map[string]string
		want Depth
	}{
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, Color256},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal", "COLORTERM": "truecolor"}, Truecolor},
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, Truecolor},
		{map[string]string{}, Truecolor},
		{map[string]string{"TERM_PROGRAM": "vscode", "COLORTERM": ""}, Truecolor},
	}
	for _, c := range cases {
		if got := DetectDepth(env(c.kv)); got != c.want {
			t.Fatalf("%v: %d", c.kv, got)
		}
	}
}

func TestColor256QuantizesTheSamePalette(t *testing.T) {
	defer func(d Depth) { ColorDepth = d }(ColorDepth)
	ColorDepth = Color256
	if got := FG(RGB{253, 207, 66}); got != "\033[38;5;221m" {
		t.Fatalf("body yellow -> %q", got)
	}
	if got := Index256(RGB{0, 0, 0}); got != 16 {
		t.Fatalf("black %d", got)
	}
	if got := Index256(RGB{17, 17, 17}); got != 233 {
		t.Fatalf("near gray rounds to the closest step: %d", got)
	}
	if got := Index256(RGB{128, 128, 128}); got != 244 {
		t.Fatalf("gray %d", got)
	}
	if got := BG(RGB{18, 42, 58}); !strings.HasPrefix(got, "\033[48;5;") {
		t.Fatalf("%q", got)
	}
	pal, err := PaletteFor(DefaultPalette)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := RenderRows(MustSprite("idle").Rows, pal, true)
	out := strings.Join(lines, "\n")
	if err != nil || strings.Contains(out, "38;2;") || !strings.Contains(out, "38;5;") {
		t.Fatalf("256 mode leaked truecolor: %v", err)
	}
}

// Bold is the only attribute Rex adds (docs/ux.md §5), and only in color;
// nothing is filled.
func TestPaintStyleBold(t *testing.T) {
	fg := UI["title"]
	if got := PaintStyle("PLAN", fg, true, false); got != "PLAN" {
		t.Fatalf("no color, no SGR: %q", got)
	}
	if got, want := PaintStyle("PLAN", fg, true, true), FG(fg)+"\033[1m"+"PLAN"+Reset; got != want {
		t.Fatalf("%q want %q", got, want)
	}
	if got := PaintStyle("x", fg, false, true); strings.Contains(got, "\033[1m") || strings.Contains(got, "\033[48") {
		t.Fatalf("no bold, no fill: %q", got)
	}
}

// docs/ux.md §5: one meaning per role. The selection is purple, xterm 141
// under 256 colors; the title is bright, never the pin's yellow; and the
// roles that only filled or tinted are gone.
func TestUIRoles(t *testing.T) {
	if got := UI["pick"]; got != (RGB{175, 135, 255}) || Index256(got) != 141 {
		t.Fatalf("pick %v -> %d", got, Index256(got))
	}
	if UI["title"] == UI["accent"] || UI["title"] != (RGB{240, 244, 250}) {
		t.Fatalf("title %v", UI["title"])
	}
	for _, gone := range []string{"pickbg", "folder", "you"} {
		if _, ok := UI[gone]; ok {
			t.Errorf("%s is retired", gone)
		}
	}
	defer func(d Depth) { ColorDepth = d }(ColorDepth)
	ColorDepth = Color256
	if got := FG(UI["pick"]); got != "\033[38;5;141m" {
		t.Fatalf("pick under 256 colors: %q", got)
	}
}
