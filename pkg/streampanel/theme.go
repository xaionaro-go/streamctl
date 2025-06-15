package streampanel

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type themeWrapperNoPaddingsAndScrollbar struct {
	fyne.Theme
}

func (t *themeWrapperNoPaddingsAndScrollbar) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 0
	case theme.SizeNameSeparatorThickness:
		return 0
	case theme.SizeNameScrollBar:
		return 0
	case theme.SizeNameScrollBarRadius:
		return 0
	case theme.SizeNameScrollBarSmall:
		return 0
	default:
		return t.Theme.Size(name)
	}
}
