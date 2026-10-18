package mascot

import (
	"fmt"
	"os"
	"strings"
)

const Reset = "\033[0m"

func ColorEnabledDefault() bool {
	return ColorEnabled(os.Stdout, nil)
}

func ColorEnabled(stdout *os.File, force *bool) bool {
	if force != nil {
		return *force
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	if stdout == nil {
		return false
	}
	info, err := stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func ColorEnabledWriter(isTTY bool) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return isTTY
}

func FG(rgb RGB) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

func BG(rgb RGB) string {
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

func Paint(text string, rgb RGB, enabled bool) string {
	if !enabled {
		return text
	}
	return FG(rgb) + text + Reset
}

func pixelRGB(ch rune, palette Palette) (*RGB, error) {
	if ch == '.' || ch == ' ' {
		return nil, nil
	}
	role, ok := PixelRoles[ch]
	if !ok {
		return nil, fmt.Errorf("unknown pixel '%c'", ch)
	}
	rgb := palette[role]
	return &rgb, nil
}

func pair(upper, lower rune, palette Palette, enabled bool) (string, error) {
	up, err := pixelRGB(upper, palette)
	if err != nil {
		return "", err
	}
	lo, err := pixelRGB(lower, palette)
	if err != nil {
		return "", err
	}
	if up == nil && lo == nil {
		return " ", nil
	}
	if !enabled {
		if up != nil && lo != nil {
			return "█", nil
		}
		if up != nil {
			return "▀", nil
		}
		return "▄", nil
	}
	if up != nil && lo != nil {
		if *up == *lo {
			return FG(*up) + BG(*up) + "▀" + Reset, nil
		}
		return FG(*up) + BG(*lo) + "▀" + Reset, nil
	}
	if up != nil {
		return FG(*up) + "▀" + Reset, nil
	}
	return FG(*lo) + "▄" + Reset, nil
}

func RenderRows(rows []string, palette Palette, enabled bool) ([]string, error) {
	width, height, err := SpriteSize(rows)
	if err != nil {
		return nil, err
	}
	padded := rows
	if height%2 == 1 {
		padded = append(append([]string{}, rows...), strings.Repeat(".", width))
	}
	lines := make([]string, 0, len(padded)/2)
	for y := 0; y < len(padded); y += 2 {
		var b strings.Builder
		for x := 0; x < width; x++ {
			cell, err := pair(runeAt(padded[y], x), runeAt(padded[y+1], x), palette, enabled)
			if err != nil {
				return nil, err
			}
			b.WriteString(cell)
		}
		lines = append(lines, b.String())
	}
	return lines, nil
}

func RenderSprite(sprite Sprite, color string, enabled bool) (string, error) {
	if color == "" {
		color = DefaultPalette
	}
	return RenderGrid(sprite.Rows, color, enabled)
}

func RenderGrid(rows []string, color string, enabled bool) (string, error) {
	pal, err := PaletteFor(color)
	if err != nil {
		return "", err
	}
	lines, err := RenderRows(rows, pal, enabled)
	if err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func MustRenderRows(rows []string, palette Palette, enabled bool) []string {
	lines, err := RenderRows(rows, palette, enabled)
	if err != nil {
		panic(err)
	}
	return lines
}

func runeAt(s string, i int) rune {
	r := []rune(s)
	if i < 0 || i >= len(r) {
		return '.'
	}
	return r[i]
}
