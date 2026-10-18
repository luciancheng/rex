package mascot

import (
	"fmt"

	"github.com/luciancheng/rex/assets"
)

const BreakdanceHold = 3

func init() {
	img, err := LoadPNGBytes(assets.BreakdancePNG, "assets/breakdance.png")
	if err != nil {
		panic(err)
	}
	frames, err := SliceSheet(img, StageSize)
	if err != nil {
		panic(err)
	}
	pal := MustPalette(DefaultPalette)
	var anim []Frame
	for i, pixels := range frames {
		rows := PixelsFromRGBA(pixels, pal)
		if !frameHasInk(rows) {
			continue
		}
		if err := ValidatePixels("breakdance", rows); err != nil {
			panic(err)
		}
		w, h, err := SpriteSize(rows)
		if err != nil {
			panic(err)
		}
		if w != StageSize || h != StageSize {
			panic(fmt.Sprintf("breakdance frame %d is %dx%d", i, w, h))
		}
		name := fmt.Sprintf("dance_%02d", i)
		RegisterSprite(Sprite{Name: name, Rows: rows})
		anim = append(anim, Frame{Sprite: name, Hold: BreakdanceHold})
	}
	if len(anim) == 0 {
		panic("breakdance sheet had no frames")
	}
	RegisterAnimation(Animation{Name: "breakdance", Frames: anim})
}
