package streampanel

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

type themeWrapperNoPaddings struct {
	fyne.Theme
}

func (t *themeWrapperNoPaddings) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 0
	default:
		return t.Theme.Size(name)
	}
}
