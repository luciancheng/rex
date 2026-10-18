package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luciancheng/rex/internal/mascot"
)

var roleLetters = []struct {
	role   string
	letter string
}{
	{"outline", "k"},
	{"dark", "d"},
	{"mid", "n"},
	{"accent", "a"},
	{"body", "y"},
	{"highlight", "Y"},
	{"shade", "o"},
	{"blush", "p"},
	{"belly", "b"},
	{"shadow", "s"},
	{"eye", "e"},
	{"white", "w"},
	{"mark", "m"},
	{"green", "g"},
	{"red", "r"},
	{"teal", "t"},
}

func main() {
	keepBG := flag.Bool("keep-background", false, "do not treat white / the most common color as transparent")
	apply := flag.Bool("apply", false, "write the result into the idle sprite and yellow palette")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: importsprite [--apply] [--keep-background] <png>")
		os.Exit(2)
	}
	pngPath := flag.Arg(0)
	rows, letterToRGB, summary, image, err := analyze(pngPath, !*keepBG)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s  %dx%d\n", pngPath, image.Width, image.Height)
	fmt.Printf("opaque colors: %d\n\n", len(summary))
	fmt.Println("letter   count   rgb")
	for _, item := range summary {
		fmt.Printf("  %s      %4d   %v\n", item.letter, item.count, item.rgb)
	}
	fmt.Println("\ngrid:")
	for _, row := range rows {
		fmt.Println(row)
	}
	fmt.Println()
	fmt.Println(formatRows(rows))
	if *apply {
		root, err := repoRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := applyIdle(root, rows, letterToRGB); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("\nwrote idle sprite to %s\n", filepath.Join(root, "internal", "mascot", "sprites.go"))
		fmt.Printf("wrote yellow palette to %s\n", filepath.Join(root, "internal", "mascot", "palettes.go"))
	}
}

type colorCount struct {
	letter string
	rgb    [3]int
	count  int
}

func analyze(path string, whiteIsTransparent bool) ([]string, map[string][3]int, []colorCount, mascot.PngImage, error) {
	image, err := mascot.LoadPNG(path)
	if err != nil {
		return nil, nil, nil, mascot.PngImage{}, err
	}
	background := guessBackground(image.Pixels, whiteIsTransparent)
	counts := map[[3]int]int{}
	for _, row := range image.Pixels {
		for _, pixel := range row {
			if isTransparent(pixel, background) {
				continue
			}
			rgb := [3]int{pixel[0], pixel[1], pixel[2]}
			counts[rgb]++
		}
	}
	colorToLetter, err := assignLetters(counts)
	if err != nil {
		return nil, nil, nil, mascot.PngImage{}, err
	}
	letterToRGB := map[string][3]int{}
	for rgb, letter := range colorToLetter {
		letterToRGB[letter] = rgb
	}
	type pair struct {
		rgb   [3]int
		count int
	}
	var ranked []pair
	for rgb, n := range counts {
		ranked = append(ranked, pair{rgb, n})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count == ranked[j].count {
			return luminance(ranked[i].rgb) < luminance(ranked[j].rgb)
		}
		return ranked[i].count > ranked[j].count
	})
	var summary []colorCount
	for _, item := range ranked {
		summary = append(summary, colorCount{letter: colorToLetter[item.rgb], rgb: item.rgb, count: item.count})
	}
	var rows []string
	for _, row := range image.Pixels {
		var chars []byte
		for _, pixel := range row {
			if isTransparent(pixel, background) {
				chars = append(chars, '.')
			} else {
				chars = append(chars, colorToLetter[[3]int{pixel[0], pixel[1], pixel[2]}][0])
			}
		}
		rows = append(rows, string(chars))
	}
	return rows, letterToRGB, summary, image, nil
}

func isTransparent(pixel [4]int, background *[3]int) bool {
	if pixel[3] < 16 {
		return true
	}
	if background != nil && pixel[0] == background[0] && pixel[1] == background[1] && pixel[2] == background[2] {
		return true
	}
	return false
}

func guessBackground(pixels [][][4]int, useWhite bool) *[3]int {
	opaque := map[[3]int]int{}
	for _, row := range pixels {
		for _, pixel := range row {
			if pixel[3] >= 16 {
				opaque[[3]int{pixel[0], pixel[1], pixel[2]}]++
			}
		}
	}
	if len(opaque) == 0 {
		return nil
	}
	var most [3]int
	best := -1
	for rgb, n := range opaque {
		if n > best {
			best = n
			most = rgb
		}
	}
	if useWhite && most == [3]int{255, 255, 255} {
		return &most
	}
	if useWhite {
		white := [3]int{255, 255, 255}
		if opaque[white] > 0 {
			return &white
		}
		return &most
	}
	return &most
}

func luminance(rgb [3]int) float64 {
	return 0.2126*float64(rgb[0]) + 0.7152*float64(rgb[1]) + 0.0722*float64(rgb[2])
}

func assignLetters(counts map[[3]int]int) (map[[3]int]string, error) {
	remaining := map[[3]int]bool{}
	for rgb := range counts {
		remaining[rgb] = true
	}
	assigned := map[[3]int]string{}
	letterFor := map[string]string{}
	for _, item := range roleLetters {
		letterFor[item.role] = item.letter
	}
	take := func(role string, color *[3]int) {
		if color == nil {
			return
		}
		if !remaining[*color] {
			return
		}
		assigned[*color] = letterFor[role]
		delete(remaining, *color)
	}
	if len(remaining) > 0 {
		c := minBy(remaining, luminance)
		take("outline", &c)
	}
	var bluish [][3]int
	for c := range remaining {
		if c[2] >= c[0] && c[2] >= c[1] {
			bluish = append(bluish, c)
		}
	}
	if len(bluish) > 0 {
		c := bluish[0]
		for _, item := range bluish[1:] {
			if luminance(item) < luminance(c) {
				c = item
			}
		}
		take("shadow", &c)
	}
	if len(remaining) > 0 {
		var body [3]int
		best := -1
		for c := range remaining {
			if counts[c] > best {
				best = counts[c]
				body = c
			}
		}
		take("body", &body)
	}
	if len(remaining) > 0 {
		c := maxBy(remaining, luminance)
		take("highlight", &c)
	}
	var pink [][3]int
	for c := range remaining {
		if c[0] > 200 && c[2] > 55 && c[1] < 160 {
			pink = append(pink, c)
		}
	}
	if len(pink) > 0 {
		c := pink[0]
		for _, item := range pink[1:] {
			if item[2] > c[2] {
				c = item
			}
		}
		take("blush", &c)
	}
	leftovers := keys(remaining)
	sort.Slice(leftovers, func(i, j int) bool { return luminance(leftovers[i]) < luminance(leftovers[j]) })
	rolesLeft := []string{"dark", "shade", "mid", "accent", "belly", "eye", "white", "mark", "green", "red", "teal"}
	for i, color := range leftovers {
		if i >= len(rolesLeft) {
			break
		}
		c := color
		take(rolesLeft[i], &c)
	}
	if len(remaining) > 0 {
		return nil, fmt.Errorf("too many unique colors for the available pixel letters")
	}
	return assigned, nil
}

func keys(m map[[3]int]bool) [][3]int {
	out := make([][3]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func minBy(m map[[3]int]bool, key func([3]int) float64) [3]int {
	var best [3]int
	first := true
	var bestV float64
	for c := range m {
		v := key(c)
		if first || v < bestV {
			best, bestV, first = c, v, false
		}
	}
	return best
}

func maxBy(m map[[3]int]bool, key func([3]int) float64) [3]int {
	var best [3]int
	first := true
	var bestV float64
	for c := range m {
		v := key(c)
		if first || v > bestV {
			best, bestV, first = c, v, false
		}
	}
	return best
}

func formatRows(rows []string) string {
	var b strings.Builder
	b.WriteString("\tidle := Pixels(\n")
	for _, row := range rows {
		b.WriteString("\t\t\"")
		b.WriteString(row)
		b.WriteString("\",\n")
	}
	b.WriteString("\t)")
	return b.String()
}

func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod")
		}
		dir = parent
	}
}

func applyIdle(root string, rows []string, letterToRGB map[string][3]int) error {
	spritesPath := filepath.Join(root, "internal", "mascot", "sprites.go")
	palettesPath := filepath.Join(root, "internal", "mascot", "palettes.go")
	sprites, err := os.ReadFile(spritesPath)
	if err != nil {
		return err
	}
	updated, err := replaceBlock(string(sprites), "\tidle := Pixels(", "\t)", formatRows(rows))
	if err != nil {
		return err
	}
	if err := os.WriteFile(spritesPath, []byte(updated), 0o644); err != nil {
		return err
	}
	palettes, err := os.ReadFile(palettesPath)
	if err != nil {
		return err
	}
	text := string(palettes)
	yellowStart := strings.Index(text, `"yellow": {`)
	if yellowStart < 0 {
		return fmt.Errorf("could not find yellow palette")
	}
	open := strings.Index(text[yellowStart:], "{")
	if open < 0 {
		return fmt.Errorf("could not find yellow palette")
	}
	open += yellowStart
	depth := 0
	end := -1
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return fmt.Errorf("could not find yellow palette")
	}
	block := text[open : end+1]
	roleForLetter := map[string]string{}
	for _, item := range roleLetters {
		roleForLetter[item.letter] = item.role
	}
	for letter, rgb := range letterToRGB {
		role := roleForLetter[letter]
		needle := fmt.Sprintf(`"%s":`, role)
		idx := strings.Index(block, needle)
		value := fmt.Sprintf("{%d, %d, %d}", rgb[0], rgb[1], rgb[2])
		if idx < 0 {
			block = block[:len(block)-1] + fmt.Sprintf("\t\t\"%s\": %s,\n\t", role, value) + "}"
			continue
		}
		brace := strings.Index(block[idx:], "{")
		close := strings.Index(block[idx:], "}")
		if brace < 0 || close < 0 {
			continue
		}
		start := idx + brace
		stop := idx + close
		block = block[:start] + value + block[stop+1:]
	}
	text = text[:open] + block + text[end+1:]
	return os.WriteFile(palettesPath, []byte(text), 0o644)
}

func replaceBlock(text, begin, end, replacement string) (string, error) {
	start := strings.Index(text, begin)
	if start < 0 {
		return "", fmt.Errorf("could not find %q", begin)
	}
	stop := strings.Index(text[start:], end)
	if stop < 0 {
		return "", fmt.Errorf("could not find closing %q", end)
	}
	stop = start + stop
	return text[:start] + replacement + text[stop+len(end):], nil
}
