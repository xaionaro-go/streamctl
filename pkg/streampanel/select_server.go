package streampanel

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type selectServerUI struct {
	*Panel
	CanvasObject fyne.CanvasObject
}

func newSelectServerUI(
	p *Panel,
) *selectServerUI {
	ui := &selectServerUI{
		Panel: p,
	}

	serversList := widget.NewList(
		ui.serverLength,
		ui.serverItemCreate,
		ui.serverItemUpdate,
	)
	serversList.OnSelected = func(id widget.ListItemID) {
		ui.onServerSelect(id)
	}
	serversList.OnUnselected = func(id widget.ListItemID) {
		ui.onSelectUnselect(id)
	}

	ui.CanvasObject = container.NewVScroll(container.NewBorder(
		nil,
		nil,
		nil,
		nil,
		serversList,
	))
	return ui
}

func (*selectServerUI) serverLength() int {
	panic("not implemented, yet")
}

func (*selectServerUI) serverItemCreate() fyne.CanvasObject {
	panic("not implemented, yet")
}

func (*selectServerUI) serverItemUpdate(
	widget.ListItemID,
	fyne.CanvasObject,
) {
	panic("not implemented, yet")
}

func (*selectServerUI) onServerSelect(
	id widget.ListItemID,
) {
	panic("not implemented, yet")
}
func (*selectServerUI) onSelectUnselect(
	id widget.ListItemID,
) {
	panic("not implemented, yet")
}
