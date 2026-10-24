package mascot

import (
	"fmt"
	"os"
	"strings"
)

const Reset = "\033[0m"

func ColorEnabledWriter(isTTY bool) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return isTTY
}

// Depth is how many colors the terminal can show.
type Depth int

const (
	Truecolor Depth = iota
	Color256
)

// ColorDepth is set once at startup. The palette never changes: on a
// 256-color terminal (Terminal.app before macOS 26) the same yellow
// palette is quantized to the nearest xterm-256 color.
var ColorDepth = Truecolor

// DetectDepth picks Color256 only for Terminal.app without truecolor;
// everything else (including COLORTERM unset over SSH or tmux) keeps
// truecolor, which is what every other supported terminal renders.
func DetectDepth(getenv func(string) string) Depth {
	ct := strings.ToLower(getenv("COLORTERM"))
	if getenv("TERM_PROGRAM") == "Apple_Terminal" && ct != "truecolor" && ct != "24bit" {
		return Color256
	}
	return Truecolor
}

func FG(rgb RGB) string {
	if ColorDepth == Color256 {
		return fmt.Sprintf("\033[38;5;%dm", Index256(rgb))
	}
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

func BG(rgb RGB) string {
	if ColorDepth == Color256 {
		return fmt.Sprintf("\033[48;5;%dm", Index256(rgb))
	}
	return fmt.Sprintf("\033[48;2;%d;%d;%dm", rgb[0], rgb[1], rgb[2])
}

var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// Index256 is the xterm-256 color closest to rgb: the 6x6x6 cube or the
// 24-step gray ramp, whichever is nearer.
func Index256(rgb RGB) int {
	nearest := func(v int) int {
		best, bestD := 0, 1<<30
		for i, l := range cubeLevels {
			if d := (v - l) * (v - l); d < bestD {
				best, bestD = i, d
			}
		}
		return best
	}
	r, g, b := nearest(rgb[0]), nearest(rgb[1]), nearest(rgb[2])
	cube := 16 + 36*r + 6*g + b
	cr, cg, cb := cubeLevels[r], cubeLevels[g], cubeLevels[b]
	cubeD := sq(rgb[0]-cr) + sq(rgb[1]-cg) + sq(rgb[2]-cb)
	avg := (rgb[0] + rgb[1] + rgb[2]) / 3
	gi := (avg - 3) / 10 // the nearest gray step 8+10k
	if gi < 0 {
		gi = 0
	}
	if gi > 23 {
		gi = 23
	}
	gv := 8 + 10*gi
	grayD := sq(rgb[0]-gv) + sq(rgb[1]-gv) + sq(rgb[2]-gv)
	if grayD < cubeD {
		return 232 + gi
	}
	return cube
}

func sq(v int) int { return v * v }

func Paint(text string, rgb RGB, enabled bool) string {
	if !enabled {
		return text
	}
	return FG(rgb) + text + Reset
}

func PaintFill(text string, fg, bg RGB, enabled bool) string {
	if !enabled {
		return text
	}
	return FG(fg) + BG(bg) + text + Reset
}

// PaintStyle is Paint with an optional fill and bold, the only attribute
// Rex adds (docs/ux.md §5). Under 256 colors fg and bg go through the same
// map, so text painted in its own fill stays invisible.
func PaintStyle(text string, fg, bg *RGB, bold, enabled bool) string {
	if !enabled {
		return text
	}
	var b strings.Builder
	if fg != nil {
		b.WriteString(FG(*fg))
	}
	if bg != nil {
		b.WriteString(BG(*bg))
	}
	if bold {
		b.WriteString("\033[1m")
	}
	b.WriteString(text)
	b.WriteString(Reset)
	return b.String()
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
