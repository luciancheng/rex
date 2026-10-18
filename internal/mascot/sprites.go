package mascot

// Idle pixel grid. Rebuild from assets/idle.png with:
//
//	go run ./cmd/importsprite assets/idle.png --apply
//
// Blink, glance, and squat are transforms of this grid — do not hand-edit them.

func init() {
	idle := Pixels(
		"........................................",
		"........................................",
		"........................................",
		"........................................",
		"......................kk................",
		".....................knnk...............",
		"....................kddnnk..............",
		".................kkkdkkkkkkkkk..........",
		"................knnkkayyyyyyyykk........",
		"...............knndkayyyyyyyyyYyk.......",
		"...............kndkayyyyyyyyyyyYyk......",
		"...............kkkayyyyyyyyyyyyyyk......",
		"..............kndkayyyyyyyyyyyyyyyk.....",
		".............kndkoayyyyyykyyyyyyyyk.....",
		".............kndkoayyyyyykyyyyyyyyk.....",
		"..............kdkoayyyyppyyyyyyyyyk.....",
		"..............kkkoayyyyyyyyyyyyyyk......",
		".............kndkoayyyyyyyyyyyyyyk......",
		"............kndkoobayyyyyyyyyyyyk.......",
		"............kndkooobayyyyaakkkkk........",
		"............kndkoooobaaaabbbk...........",
		"............kkkooooobbbbbbkybk..........",
		"...........kndkoobbbbbbkbykyyk..........",
		"..........kndkkoobbbbbbbkyykyk..........",
		".........kkndkooobbbbbbbbkkkyk..........",
		".....kk.knkkkooooooooooooobbk...........",
		"....kyyknddkooooobbbbkooooobk...........",
		"....koyykkkooooobbbbbbkoooobk...........",
		".....koyyyooooobbbbbbbkooook............",
		".....kdoooooooobbbbbbbkkkkkk............",
		"......kdddoooookbbbbbbkbbbbk............",
		".......kkkdddkkkkbbbbddkbbddk...........",
		"..........kkkkkskddddddkddddk...........",
		"........ss.ssssskkkkkkkskkkkkss.ss......",
		".............sssssssssssssssss..........",
		"........................................",
		"........................................",
		"........................................",
		"........................................",
		"........................................",
	)
	if err := ValidatePixels("idle", idle); err != nil {
		panic(err)
	}
	IdleRows = idle
	RegisterSprite(Sprite{Name: "idle", Rows: idle})

	blink := PaintPixels(idle, map[[2]int]rune{{25, 13}: 'y'})
	look := PaintPixels(idle, map[[2]int]rune{
		{25, 13}: 'y', {25, 14}: 'y', {26, 13}: 'k', {26, 14}: 'k',
	})
	breathe := ShiftBody(idle, 1, 7)
	for name, rows := range map[string][]string{
		"idle_blink":   blink,
		"idle_look":    look,
		"idle_breathe": breathe,
	} {
		if err := ValidatePixels(name, rows); err != nil {
			panic(err)
		}
		RegisterSprite(Sprite{Name: name, Rows: rows})
	}

	RegisterAnimation(Animation{
		Name: "idle",
		Frames: []Frame{
			{"idle", 24},
			{"idle_breathe", 12},
			{"idle", 16},
			{"idle_blink", 3},
			{"idle", 18},
			{"idle_look", 14},
			{"idle", 12},
			{"idle_breathe", 10},
			{"idle", 16},
			{"idle_blink", 3},
			{"idle", 14},
			{"idle_look", 12},
		},
	})
}

// IdleRows is the 1:1 import of assets/idle.png.
var IdleRows []string
