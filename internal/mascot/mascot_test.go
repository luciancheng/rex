package mascot

import (
	"strings"
	"testing"
)

func TestIdleIsRegistered(t *testing.T) {
	sprite := MustSprite("idle")
	if sprite.Name != DefaultSprite {
		t.Fatalf("%s", sprite.Name)
	}
	width, height, err := SpriteSize(sprite.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if width != sprite.Width() || width != StageSize {
		t.Fatalf("width %d", width)
	}
	if height != sprite.Height() || height != StageSize {
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

func TestPalettesDefineEveryRole(t *testing.T) {
	for name, palette := range Palettes {
		for _, role := range PixelRoles {
			if _, ok := palette[role]; !ok {
				t.Fatalf("%s missing %s", name, role)
			}
		}
	}
}
