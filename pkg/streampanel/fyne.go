package streampanel

import (
	"time"

	"fyne.io/fyne/v2"
)

func hideWindow(w fyne.Window) {
	for i := 0; i < 10; i++ {
		time.Sleep(time.Millisecond)
		w.Hide()
	}
}
