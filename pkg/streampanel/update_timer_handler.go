package streampanel

import (
	"context"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/clock"

	"fyne.io/fyne/v2/widget"
	"github.com/xaionaro-go/observability"
)

type updateTimerHandler struct {
	cancelFn        context.CancelFunc
	startStopButton *widget.Button
	startTS         time.Time
}

func newUpdateTimerHandler(
	startStopButton *widget.Button,
	startedAt time.Time,
) *updateTimerHandler {
	ctx, cancelFn := context.WithCancel(context.Background())
	h := &updateTimerHandler{
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

func (h *updateTimerHandler) loop(ctx context.Context) {
	t := clock.Get().Ticker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			timePassed := clock.Get().Since(h.startTS).Truncate(time.Second)
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
	return clock.Get().Since(h.GetStartTS())
}

func (h *updateTimerHandler) Close() error {
	h.Stop()
	return nil
}
