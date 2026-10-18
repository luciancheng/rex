package mascot

import (
	"fmt"
	"sort"
	"strings"
)

type Sprite struct {
	Name string
	Rows []string
}

func (s Sprite) Width() int {
	if len(s.Rows) == 0 {
		return 0
	}
	return len(s.Rows[0])
}

func (s Sprite) Height() int {
	return len(s.Rows)
}

type Frame struct {
	Sprite string
	Hold   int
}

type Animation struct {
	Name   string
	Frames []Frame
}

var (
	Sprites    = map[string]Sprite{}
	Animations = map[string]Animation{}
)

const (
	DefaultSprite    = "idle"
	DefaultAnimation = "idle"
)

func RegisterSprite(sprite Sprite) Sprite {
	if len(sprite.Rows) == 0 {
		panic(fmt.Sprintf("sprite '%s' has no rows", sprite.Name))
	}
	width := sprite.Width()
	for _, row := range sprite.Rows {
		if len(row) != width {
			panic(fmt.Sprintf("sprite '%s' rows must be the same width", sprite.Name))
		}
	}
	Sprites[sprite.Name] = sprite
	return sprite
}

func RegisterAnimation(animation Animation) Animation {
	if len(animation.Frames) == 0 {
		panic(fmt.Sprintf("animation '%s' has no frames", animation.Name))
	}
	for _, frame := range animation.Frames {
		if _, ok := Sprites[frame.Sprite]; !ok {
			panic(fmt.Sprintf("animation '%s' references unknown sprite '%s'", animation.Name, frame.Sprite))
		}
		if frame.Hold < 1 {
			panic(fmt.Sprintf("animation '%s' frame '%s' hold must be >= 1", animation.Name, frame.Sprite))
		}
	}
	Animations[animation.Name] = animation
	return animation
}

func GetSprite(name string) (Sprite, error) {
	sprite, ok := Sprites[name]
	if !ok {
		known := "(none)"
		if len(Sprites) > 0 {
			keys := make([]string, 0, len(Sprites))
			for k := range Sprites {
				keys = append(keys, k)
			}
			known = strings.Join(sortedCopy(keys), ", ")
		}
		return Sprite{}, fmt.Errorf("unknown sprite '%s'. known: %s", name, known)
	}
	return sprite, nil
}

func GetAnimation(name string) (Animation, error) {
	anim, ok := Animations[name]
	if !ok {
		known := "(none)"
		if len(Animations) > 0 {
			keys := make([]string, 0, len(Animations))
			for k := range Animations {
				keys = append(keys, k)
			}
			known = strings.Join(sortedCopy(keys), ", ")
		}
		return Animation{}, fmt.Errorf("unknown animation '%s'. known: %s", name, known)
	}
	return anim, nil
}

func MustSprite(name string) Sprite {
	s, err := GetSprite(name)
	if err != nil {
		panic(err)
	}
	return s
}

func MustAnimation(name string) Animation {
	a, err := GetAnimation(name)
	if err != nil {
		panic(err)
	}
	return a
}

func MustPalette(name string) Palette {
	p, err := PaletteFor(name)
	if err != nil {
		panic(err)
	}
	return p
}

func sortedCopy(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.Strings(out)
	return out
}
