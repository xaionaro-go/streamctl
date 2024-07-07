package screenshot

import (
	"github.com/kbinani/screenshot"
)

func NumActiveDisplays() uint {
	return uint(screenshot.NumActiveDisplays())
}
