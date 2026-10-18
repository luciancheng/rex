package mascot

import "github.com/luciancheng/rex/assets"

const BreakdanceHold = 3

func init() {
	RegisterSheetAnimation("breakdance", "assets/breakdance.png", assets.BreakdancePNG, BreakdanceHold)
}
