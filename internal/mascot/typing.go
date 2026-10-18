package mascot

import "github.com/luciancheng/rex/assets"

const TypingHold = 3

func init() {
	RegisterSheetAnimation("typing", "assets/typing.png", assets.TypingPNG, TypingHold)
}
