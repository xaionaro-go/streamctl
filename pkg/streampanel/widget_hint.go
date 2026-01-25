package streampanel

import (
	"context"
	"reflect"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/clock"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

type HintWidget struct {
	*widget.Label
	Locker xsync.Mutex
	Text   string
	Hint   *widget.PopUp
	Window fyne.Window

	RecheckerCancelFn context.CancelFunc
}

var _ desktop.Hoverable = (*HintWidget)(nil)

func NewHintWidget(window fyne.Window, text string) *HintWidget {
	w := &HintWidget{
		Label:  widget.NewLabel("?"),
		Window: window,
	}
	w.Text = text
	w.Hint = widget.NewPopUp(widget.NewLabel(w.Text), window.Canvas())
	w.Hint.Hide()
	w.ExtendBaseWidget(w)

	if !w.Hint.Hidden {
		panic("should not have happened")
	}
	return w
}

func (w *HintWidget) MouseIn(ev *desktop.MouseEvent) {
	ctx := context.TODO()
	w.Locker.Do(ctx, func() {
		w.mouseIn(ev)
	})
}

func (w *HintWidget) mouseIn(ev *desktop.MouseEvent) {
	if !w.Hint.Hidden {
		return
	}

	pos := ev.AbsolutePosition
	pos.X += 1
	pos.Y += 5
	w.Hint.Move(pos)
	w.Hint.Show()

	recheckerTicker := clock.Get().Ticker(100 * time.Millisecond)
	ctx, cancelFn := context.WithCancel(context.Background())
	if w.RecheckerCancelFn != nil {
		panic("should not have happened")
	}
	w.RecheckerCancelFn = cancelFn
	observability.Go(ctx, func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				recheckerTicker.Stop()
				return
			case <-recheckerTicker.C:
			}

			mousePos := getMousePos(w.Window)
			if !w.isHovering(mousePos) {
				w.hideHint()
				return
			}

			pos := mousePos
			pos.X += 1
			pos.Y += 5
			w.Hint.Move(pos)
		}
	})
}
func (w *HintWidget) MouseMoved(*desktop.MouseEvent) {
}

func fyneFindParent(root fyne.CanvasObject, target fyne.CanvasObject) fyne.CanvasObject {
	switch obj := root.(type) {
	case *fyne.Container:
		for _, child := range obj.Objects {
			if child == target {
				return obj
			}
			if parent := fyneFindParent(child, target); parent != nil {
				return parent
			}
		}
	}
	return nil
}

// GetAbsolutePosition calculates the absolute position of a widget.
func GetAbsolutePosition(w, root fyne.CanvasObject) fyne.Position {
	pos := w.Position()
	for parent := fyneFindParent(root, w); parent != nil; parent = fyneFindParent(root, parent) {
		pos = pos.Add(parent.Position())
	}
	return pos
}

func getMousePos(window fyne.Window) fyne.Position {
	return *unsafetools.FieldByNameInValue(reflect.ValueOf(window), "mousePos").Interface().(*fyne.Position)
}

func (w *HintWidget) isHovering(mousePos fyne.Position) bool {
	pos0 := GetAbsolutePosition(w, w.Window.Canvas().Content())
	pos1 := pos0.Add(w.Label.Size())
	if mousePos.X >= pos0.X && mousePos.Y >= pos0.Y && mousePos.X <= pos1.X &&
		mousePos.Y <= pos1.Y {
		return true
	}

	return false
}

func (w *HintWidget) hideHint() {
	ctx := context.TODO()
	w.Locker.Do(ctx, func() {
		if w.Hint.Hidden {
			return
		}

		w.Hint.Hide()
		if w.RecheckerCancelFn == nil {
			panic("should not have happened")
		}
		w.RecheckerCancelFn()
		w.RecheckerCancelFn = nil
	})
}

func (w *HintWidget) MouseOut() {
	if w.Hint.Hidden {
		return
	}

	if w.isHovering(getMousePos(w.Window)) {
		return
	}

	w.hideHint()
}
