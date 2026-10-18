package mascot

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type PngImage struct {
	Width  int
	Height int
	Pixels [][][4]int
}

func LoadPNG(path string) (PngImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PngImage{}, err
	}
	return LoadPNGBytes(data, path)
}

func LoadPNGBytes(data []byte, name string) (PngImage, error) {
	if name == "" {
		name = "png"
	}
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return PngImage{}, fmt.Errorf("%s is not a PNG", name)
	}
	var width, height, bitDepth, colorType int
	var haveIHDR bool
	var idat []byte
	var palette [][3]int
	var alphaByIndex []int
	i := 8
	for i+12 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[i : i+4]))
		tag := string(data[i+4 : i+8])
		if i+8+length+4 > len(data) {
			break
		}
		chunk := data[i+8 : i+8+length]
		switch tag {
		case "IHDR":
			width = int(binary.BigEndian.Uint32(chunk[0:4]))
			height = int(binary.BigEndian.Uint32(chunk[4:8]))
			bitDepth = int(chunk[8])
			colorType = int(chunk[9])
			haveIHDR = true
		case "PLTE":
			for n := 0; n+2 < len(chunk); n += 3 {
				palette = append(palette, [3]int{int(chunk[n]), int(chunk[n+1]), int(chunk[n+2])})
			}
		case "tRNS":
			for _, b := range chunk {
				alphaByIndex = append(alphaByIndex, int(b))
			}
		case "IDAT":
			idat = append(idat, chunk...)
		case "IEND":
			i = len(data)
			continue
		}
		i += 12 + length
	}
	if !haveIHDR {
		return PngImage{}, fmt.Errorf("%s is missing IHDR", name)
	}
	rawReader, err := zlib.NewReader(bytes.NewReader(idat))
	if err != nil {
		return PngImage{}, err
	}
	defer rawReader.Close()
	raw, err := io.ReadAll(rawReader)
	if err != nil {
		return PngImage{}, err
	}
	channels := map[int]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[colorType]
	rowBytes := (width*bitDepth*channels + 7) / 8
	filterBpp := max(1, (bitDepth*channels+7)/8)
	rows := make([][]byte, 0, height)
	prev := make([]byte, rowBytes)
	offset := 0
	for y := 0; y < height; y++ {
		if offset+1+rowBytes > len(raw) {
			return PngImage{}, fmt.Errorf("%s truncated IDAT", name)
		}
		filterType := raw[offset]
		scan := append([]byte{}, raw[offset+1:offset+1+rowBytes]...)
		offset += 1 + rowBytes
		if err := paethUnfilter(int(filterType), scan, prev, filterBpp); err != nil {
			return PngImage{}, err
		}
		rows = append(rows, append([]byte{}, scan...))
		copy(prev, scan)
	}
	pixels, err := toRGBA(rows, width, height, bitDepth, colorType, palette, alphaByIndex)
	if err != nil {
		return PngImage{}, err
	}
	return PngImage{Width: width, Height: height, Pixels: pixels}, nil
}

func paethUnfilter(filterType int, scan, prev []byte, bpp int) error {
	if filterType == 0 {
		return nil
	}
	for x, value := range scan {
		left := 0
		if x >= bpp {
			left = int(scan[x-bpp])
		}
		up := int(prev[x])
		upLeft := 0
		if x >= bpp {
			upLeft = int(prev[x-bpp])
		}
		var recon int
		switch filterType {
		case 1:
			recon = left
		case 2:
			recon = up
		case 3:
			recon = (left + up) / 2
		case 4:
			estimate := left + up - upLeft
			dl, du, dul := abs(estimate-left), abs(estimate-up), abs(estimate-upLeft)
			switch {
			case dl <= du && dl <= dul:
				recon = left
			case du <= dul:
				recon = up
			default:
				recon = upLeft
			}
		default:
			return fmt.Errorf("unsupported PNG filter %d", filterType)
		}
		scan[x] = byte((int(value) + recon) & 255)
	}
	return nil
}

func toRGBA(rows [][]byte, width, height, bitDepth, colorType int, palette [][3]int, alphaByIndex []int) ([][][4]int, error) {
	channels := map[int]int{0: 1, 2: 3, 3: 1, 4: 2, 6: 4}[colorType]
	out := make([][][4]int, height)
	for y := 0; y < height; y++ {
		samples := unpackSamples(rows[y], width, bitDepth, channels)
		row := make([][4]int, 0, width)
		switch colorType {
		case 3:
			for _, index := range samples {
				r, g, b := palette[index][0], palette[index][1], palette[index][2]
				a := 255
				if index < len(alphaByIndex) {
					a = alphaByIndex[index]
				}
				row = append(row, [4]int{r, g, b, a})
			}
		case 2:
			for i := 0; i < len(samples); i += 3 {
				row = append(row, [4]int{samples[i], samples[i+1], samples[i+2], 255})
			}
		case 6:
			for i := 0; i < len(samples); i += 4 {
				row = append(row, [4]int{samples[i], samples[i+1], samples[i+2], samples[i+3]})
			}
		default:
			return nil, fmt.Errorf("unsupported PNG color type %d", colorType)
		}
		out[y] = row
	}
	return out, nil
}

func unpackSamples(scan []byte, width, bitDepth, channels int) []int {
	if bitDepth == 8 {
		n := width * channels
		out := make([]int, n)
		for i := 0; i < n && i < len(scan); i++ {
			out[i] = int(scan[i])
		}
		return out
	}
	mask := (1 << bitDepth) - 1
	perByte := 8 / bitDepth
	values := make([]int, 0, width*channels)
	for _, b := range scan {
		for shift := perByte - 1; shift >= 0; shift-- {
			values = append(values, (int(b)>>(shift*bitDepth))&mask)
			if len(values) == width*channels {
				return values
			}
		}
	}
	return values[:min(len(values), width*channels)]
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
