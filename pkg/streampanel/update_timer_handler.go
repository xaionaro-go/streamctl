package streampanel

import (
	"context"
	"time"

	"fyne.io/fyne/v2/widget"
)

type updateTimerHandler struct {
	ctx             context.Context
	cancelFn        context.CancelFunc
	startStopButton *widget.Button
	startTS         time.Time
}

func newUpdateTimerHandler(startStopButton *widget.Button) *updateTimerHandler {
	ctx, cancelFn := context.WithCancel(context.Background())
	h := &updateTimerHandler{
		ctx:             ctx,
		cancelFn:        cancelFn,
		startStopButton: startStopButton,
		startTS:         time.Now(),
	}
	h.startStopButton.Text = "0s"
	go h.loop()
	return h
}

func (h *updateTimerHandler) GetStartTS() time.Time {
	return h.startTS
}

func (h *updateTimerHandler) loop() {
	t := time.NewTicker(time.Second)
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
			timePassed := time.Since(h.startTS).Truncate(time.Second)
			h.startStopButton.Text = timePassed.String()
			h.startStopButton.Refresh()
		}
	}
}

func (h *updateTimerHandler) Stop() time.Duration {
	h.cancelFn()
	return time.Since(h.GetStartTS())
}

func (h *updateTimerHandler) Close() error {
	h.Stop()
	return nil
}
