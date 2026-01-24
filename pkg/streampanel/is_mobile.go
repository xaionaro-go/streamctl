package streampanel

import (
	"fyne.io/fyne/v2"
)

var isMobile = func() bool {
	return fyne.CurrentDevice().IsMobile()
}
