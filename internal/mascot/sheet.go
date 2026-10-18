package mascot

import "fmt"

func dist2(a, b RGB) int {
	dr, dg, db := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return dr*dr + dg*dg + db*db
}

func nearestLetter(rgb RGB, pal Palette) rune {
	best := '.'
	bestD := -1
	for _, ch := range []rune{'k', 'y', 'Y', 'o', 'd', 'n', 'a', 'b', 'p', 's', 'w', 'm', 'g', 'r', 't', 'e'} {
		role, ok := PixelRoles[ch]
		if !ok {
			continue
		}
		target, ok := pal[role]
		if !ok {
			continue
		}
		d := dist2(rgb, target)
		if bestD < 0 || d < bestD {
			bestD = d
			best = ch
		}
	}
	return best
}

func isBlankPixel(p [4]int) bool {
	if p[3] < 16 {
		return true
	}
	r, g, b := p[0], p[1], p[2]
	if b >= r && b >= 36 && r < 90 {
		return false
	}
	return r < 22 && g < 18 && b < 18
}

func PixelsFromRGBA(pixels [][][4]int, pal Palette) []string {
	if len(pixels) == 0 {
		return nil
	}
	h := len(pixels)
	w := len(pixels[0])
	ink := make([][]bool, h)
	for y := 0; y < h; y++ {
		ink[y] = make([]bool, w)
		for x := 0; x < w; x++ {
			ink[y][x] = !isBlankPixel(pixels[y][x])
		}
	}
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		line := make([]rune, w)
		for x := 0; x < w; x++ {
			p := pixels[y][x]
			if p[3] < 16 {
				line[x] = '.'
				continue
			}
			if isBlankPixel(p) {
				outline := false
				for dy := -1; dy <= 1 && !outline; dy++ {
					for dx := -1; dx <= 1; dx++ {
						ny, nx := y+dy, x+dx
						if ny < 0 || nx < 0 || ny >= h || nx >= w || (dx == 0 && dy == 0) {
							continue
						}
						if ink[ny][nx] {
							outline = true
							break
						}
					}
				}
				if outline {
					line[x] = 'k'
				} else {
					line[x] = '.'
				}
				continue
			}
			line[x] = nearestLetter(RGB{p[0], p[1], p[2]}, pal)
		}
		rows[y] = string(line)
	}
	return rows
}

func SliceSheet(img PngImage, cell int) ([][][][4]int, error) {
	if cell < 1 {
		return nil, fmt.Errorf("cell size must be >= 1")
	}
	if img.Width%cell != 0 || img.Height%cell != 0 {
		return nil, fmt.Errorf("sheet %dx%d is not a multiple of %d", img.Width, img.Height, cell)
	}
	cols := img.Width / cell
	rows := img.Height / cell
	var frames [][][][4]int
	for gy := 0; gy < rows; gy++ {
		for gx := 0; gx < cols; gx++ {
			frame := make([][][4]int, cell)
			for y := 0; y < cell; y++ {
				row := make([][4]int, cell)
				copy(row, img.Pixels[gy*cell+y][gx*cell:gx*cell+cell])
				frame[y] = row
			}
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

func RegisterSheetAnimation(animName, pngName string, data []byte, hold int) {
	img, err := LoadPNGBytes(data, pngName)
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
		if err := ValidatePixels(animName, rows); err != nil {
			panic(err)
		}
		w, h, err := SpriteSize(rows)
		if err != nil {
			panic(err)
		}
		if w != StageSize || h != StageSize {
			panic(fmt.Sprintf("%s frame %d is %dx%d", animName, i, w, h))
		}
		name := fmt.Sprintf("%s_%02d", animName, i)
		RegisterSprite(Sprite{Name: name, Rows: rows})
		anim = append(anim, Frame{Sprite: name, Hold: hold})
	}
	if len(anim) == 0 {
		panic(animName + " sheet had no frames")
	}
	RegisterAnimation(Animation{Name: animName, Frames: anim})
}

func frameHasInk(rows []string) bool {
	for _, row := range rows {
		for _, ch := range row {
			if ch != '.' {
				return true
			}
		}
	}
	return false
}
