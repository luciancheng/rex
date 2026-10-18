package mascot

import (
	"fmt"
	"strings"
)

var PixelRoles = map[rune]string{
	'k': "outline",
	'y': "body",
	'Y': "highlight",
	'o': "shade",
	'd': "dark",
	'n': "mid",
	'a': "accent",
	'b': "belly",
	'e': "eye",
	'w': "white",
	'p': "blush",
	's': "shadow",
	'm': "mark",
	'g': "green",
	'r': "red",
	't': "teal",
}

const StageSize = 40

func Pixels(rows ...string) []string {
	cleaned := make([]string, len(rows))
	width := 0
	for i, row := range rows {
		row = strings.ReplaceAll(row, " ", ".")
		cleaned[i] = row
		if len(row) > width {
			width = len(row)
		}
	}
	out := make([]string, len(cleaned))
	for i, row := range cleaned {
		if len(row) < width {
			row += strings.Repeat(".", width-len(row))
		}
		out[i] = row
	}
	return out
}

func FitStageDefault(rows []string) []string {
	return FitStage(rows, StageSize, StageSize)
}

func FitStage(rows []string, width, height int) []string {
	if width < 0 {
		width = 0
	}
	fitted := make([]string, 0, height)
	for y := 0; y < height; y++ {
		if y < len(rows) {
			row := rows[y]
			if width < len(row) {
				row = row[:width]
			}
			if len(row) < width {
				row += strings.Repeat(".", width-len(row))
			}
			fitted = append(fitted, row)
			continue
		}
		fitted = append(fitted, strings.Repeat(".", width))
	}
	return fitted
}

func SpriteSize(rows []string) (int, int, error) {
	if len(rows) == 0 {
		return 0, 0, nil
	}
	width := len(rows[0])
	for _, row := range rows {
		if len(row) != width {
			return 0, 0, fmt.Errorf("sprite rows must be the same width")
		}
	}
	return width, len(rows), nil
}

func ValidatePixels(name string, rows []string) error {
	for y, row := range rows {
		for x, ch := range row {
			if ch != '.' {
				if _, ok := PixelRoles[ch]; !ok {
					return fmt.Errorf("%s[%d,%d] has unknown pixel %q", name, y, x, ch)
				}
			}
		}
	}
	return nil
}

func PaintPixels(base []string, updates map[[2]int]rune) []string {
	grid := make([][]rune, len(base))
	for y, row := range base {
		grid[y] = []rune(row)
	}
	for pos, ch := range updates {
		x, y := pos[0], pos[1]
		grid[y][x] = ch
	}
	out := make([]string, len(grid))
	for y, row := range grid {
		out[y] = string(row)
	}
	return out
}

func Nudge(base []string, dx, dy, keepBottom int) []string {
	if (dx == 0 && dy == 0) || keepBottom >= len(base) {
		return base
	}
	body := append([]string{}, base[:len(base)-keepBottom]...)
	shadow := append([]string{}, base[len(base)-keepBottom:]...)
	empty := strings.Repeat(".", len(base[0]))
	if dy > 0 {
		body = append(repeatString(empty, dy), body[:len(body)-dy]...)
	} else if dy < 0 {
		body = append(body[-dy:], repeatString(empty, -dy)...)
	}
	if dx > 0 {
		for i, row := range body {
			body[i] = strings.Repeat(".", dx) + row[:len(row)-dx]
		}
	} else if dx < 0 {
		for i, row := range body {
			body[i] = row[-dx:] + strings.Repeat(".", -dx)
		}
	}
	return append(body, shadow...)
}

func ShiftBody(base []string, dy, keepBottom int) []string {
	return Nudge(base, 0, dy, keepBottom)
}

func repeatString(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
