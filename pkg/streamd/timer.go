package streamd

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

type Timer struct {
	api.Timer
	xsync.Mutex
	StreamD      *StreamD
	RunningTimer *clock.Timer
}

func NewTimer(
	streamD *StreamD,
	timerID api.TimerID,
	triggerAt time.Time,
	action api.Action,
) *Timer {
	return &Timer{
		StreamD: streamD,
		Timer: api.Timer{
			ID:        timerID,
			TriggerAt: triggerAt,
			Action:    action,
		},
	}
}

func (t *Timer) Start(ctx context.Context) {
	logger.Debugf(ctx, "Start")
	defer logger.Debugf(ctx, "/Start")

	t.Do(ctx, func() {
		t.start(ctx)
	})
}

func (t *Timer) Stop(ctx context.Context) {
	logger.Debugf(ctx, "Stop")
	defer logger.Debugf(ctx, "/Stop")

	t.Do(ctx, func() {
		t.stop(ctx)
	})
}

func (t *Timer) start(ctx context.Context) {
	logger.Debugf(ctx, "start")
	defer logger.Debugf(ctx, "/start")

	if t.RunningTimer != nil {
		return
	}

	runningTimer := clock.Get().Timer(time.Until(t.TriggerAt))
	t.RunningTimer = runningTimer

	observability.Go(ctx, func(ctx context.Context) {
		select {
		case <-ctx.Done():
			t.stop(ctx)
			return
		case <-runningTimer.C:
		}
		t.Do(ctx, func() {
			t.stop(ctx)
			t.trigger(ctx)
		})
	})
}

func (t *Timer) stop(ctx context.Context) {
	logger.Debugf(ctx, "stop")
	defer logger.Debugf(ctx, "/stop")

	if t.RunningTimer == nil {
		return
	}

	t.RunningTimer.Stop()
	t.RunningTimer = nil
}

func (t *Timer) Trigger(ctx context.Context) {
	logger.Debugf(ctx, "Trigger")
	defer logger.Debugf(ctx, "/Trigger")

	t.Do(ctx, func() {
		t.trigger(ctx)
	})
}

func (t *Timer) trigger(ctx context.Context) {
	logger.Debugf(ctx, "trigger (%T)", t.Timer.Action)
	defer logger.Debugf(ctx, "/trigger (%T)", t.Timer.Action)

	observability.Go(ctx, func(ctx context.Context) {
		err := t.StreamD.RemoveTimer(ctx, t.Timer.ID)
		if err != nil {
			logger.Error(ctx, "unable to remove timer %d: %v", t.Timer.ID, err)
		}
	})

	err := t.StreamD.doAction(ctx, t.Timer.Action, nil)
	if err != nil {
		logger.Errorf(ctx, "unable to perform action %#+v: %v", t.Timer.Action, err)
	}
}
