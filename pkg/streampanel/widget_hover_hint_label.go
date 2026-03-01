package streampanel

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// hoverHintLabel is a label that shows a popup hint on mouse hover.
// Unlike HintWidget, it does not poll mouse position, so it works
// correctly inside widget.List items where absolute position
// calculation fails.
type hoverHintLabel struct {
	widget.Label
	hintText string
	popup    *widget.PopUp
	canvas   fyne.Canvas
}

var _ desktop.Hoverable = (*hoverHintLabel)(nil)

func newHoverHintLabel(canvas fyne.Canvas, labelText, hintText string) *hoverHintLabel {
	w := &hoverHintLabel{
		hintText: hintText,
		canvas:   canvas,
	}
	w.SetText(labelText)
	w.popup = widget.NewPopUp(widget.NewLabel(hintText), canvas)
	w.popup.Hide()
	w.ExtendBaseWidget(w)
	return w
}

func (w *hoverHintLabel) MouseIn(ev *desktop.MouseEvent) {
	pos := ev.AbsolutePosition
	pos.Y += 5
	w.popup.Move(pos)
	w.popup.Show()
}

func (w *hoverHintLabel) MouseMoved(ev *desktop.MouseEvent) {
	if w.popup.Hidden {
		return
	}
	pos := ev.AbsolutePosition
	pos.Y += 5
	w.popup.Move(pos)
}

func (w *hoverHintLabel) MouseOut() {
	w.popup.Hide()
}
