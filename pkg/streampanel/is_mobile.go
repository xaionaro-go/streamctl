package streampanel

import (
	"fyne.io/fyne/v2"
)

func isMobile() bool {
	return true || fyne.CurrentDevice().IsMobile()
}
