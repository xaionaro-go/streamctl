package streamd

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type Timer struct {
	api.Timer
	xsync.Mutex
	StreamD      *StreamD
	RunningTimer *time.Timer
}

func NewTimer(
	streamD *StreamD,
	timerID api.TimerID,
	triggerAt time.Time,
	action api.TimerAction,
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

	runningTimer := time.NewTimer(time.Until(t.TriggerAt))
	t.RunningTimer = runningTimer

	observability.Go(ctx, func() {
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

	observability.Go(ctx, func() {
		err := t.StreamD.RemoveTimer(ctx, t.Timer.ID)
		if err != nil {
			logger.Error(ctx, "unable to remove timer %d: %w", t.Timer.ID, err)
		}
	})

	switch action := t.Timer.Action.(type) {
	case *api.TimerActionNoop:
		return
	case *api.TimerActionStartStream:
		err := t.StreamD.StartStream(
			ctx,
			action.PlatID,
			action.Title,
			action.Description,
			action.Profile,
		)
		if err != nil {
			logger.Errorf(ctx, "unable to start stream by timer %d (%#+v): %v", t.Timer.ID, t.Timer, err)
		}
	case *api.TimerActionEndStream:
		err := t.StreamD.EndStream(
			ctx,
			action.PlatID,
		)
		if err != nil {
			logger.Errorf(ctx, "unable to end stream by timer %d (%#+v): %v", t.Timer.ID, t.Timer, err)
		}
	default:
		logger.Error(ctx, "unknown action type: %t", action)
	}
}
