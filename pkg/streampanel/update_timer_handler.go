package streampanel

import (
	"context"
	"time"

	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

type updateTimerHandler struct {
	ctx             context.Context
	cancelFn        context.CancelFunc
	startStopButton *widget.Button
	startTS         time.Time
}

func newUpdateTimerHandler(startStopButton *widget.Button, startedAt time.Time) *updateTimerHandler {
	ctx, cancelFn := context.WithCancel(context.Background())
	h := &updateTimerHandler{
		ctx:             ctx,
		cancelFn:        cancelFn,
		startStopButton: startStopButton,
		startTS:         startedAt,
	}
	h.startStopButton.Text = "..."
	observability.Go(ctx, h.loop)
	return h
}

func (h *updateTimerHandler) GetStartTS() time.Time {
	if h == nil {
		return time.Time{}
	}

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
	if h == nil {
		return 0
	}

	h.cancelFn()
	return time.Since(h.GetStartTS())
}

func (h *updateTimerHandler) Close() error {
	h.Stop()
	return nil
}
