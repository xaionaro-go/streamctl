package streampanel

import (
	"fyne.io/fyne/v2"
)

func isMobile() bool {
	return fyne.CurrentDevice().IsMobile()
}
