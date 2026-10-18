package mascot

import (
	"fmt"
	"strings"
)

type RGB [3]int

type Palette map[string]RGB

const DefaultPalette = "yellow"

var Palettes = map[string]Palette{
	"yellow": {
		"outline":   {0, 0, 0},
		"body":      {253, 207, 66},
		"highlight": {255, 219, 110},
		"shade":     {253, 150, 47},
		"dark":      {250, 113, 50},
		"mid":       {247, 156, 51},
		"accent":    {255, 158, 49},
		"belly":     {254, 162, 46},
		"eye":       {0, 0, 0},
		"white":     {255, 255, 255},
		"blush":     {253, 133, 66},
		"shadow":    {41, 58, 82},
		"mark":      {236, 240, 248},
		"green":     {88, 214, 108},
		"red":       {232, 78, 78},
		"teal":      {86, 168, 220},
	},
}

var UI = map[string]RGB{
	"title":  {244, 196, 66},
	"muted":  {132, 148, 172},
	"text":   {226, 232, 240},
	"border": {52, 68, 92},
	"accent": {244, 196, 66},
	"error":  {232, 78, 78},
	"you":    {92, 196, 232},
	"folder": {253, 150, 47},
	"action": {88, 214, 108},
	"pick":   {92, 196, 232},
	"pickbg": {18, 42, 58},
}

func PaletteFor(name string) (Palette, error) {
	pal, ok := Palettes[name]
	if !ok {
		keys := make([]string, 0, len(Palettes))
		for k := range Palettes {
			keys = append(keys, k)
		}
		return nil, fmt.Errorf("unknown color '%s'. choose from: %s", name, strings.Join(keys, ", "))
	}
	return pal, nil
}
