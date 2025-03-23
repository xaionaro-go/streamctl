package screen

import (
	"github.com/kbinani/screenshot"
)

func GetNumber() int {
	return screenshot.NumActiveDisplays()
}
