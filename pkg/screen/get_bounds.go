package screen

import (
	"image"

	"github.com/kbinani/screenshot"
)

func GetBounds(screenID int) image.Rectangle {
	return screenshot.GetDisplayBounds(screenID)
}
