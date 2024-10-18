package streampanel

import (
	"context"
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/windowmanagerhandler"
)

var hostname *string

func init() {
	if _hostname, err := os.Hostname(); err == nil {
		hostname = &_hostname
	}
}

func (p *Panel) initEventSensor(ctx context.Context) {
	es, err := newEventSensor()
	if err != nil {
		p.DisplayError(err)
		return
	}

	observability.Go(ctx, func() {
		logger.Debugf(ctx, "eventSensor")
		defer logger.Debugf(ctx, "/eventSensor")
		es.Loop(ctx, p.StreamD)
	})
}

type eventSensor struct {
	WMH *windowmanagerhandler.WindowManagerHandler

	PreviouslyFocusedWindow *windowmanagerhandler.WindowFocusChange
}

func newEventSensor() (*eventSensor, error) {
	wmh, err := windowmanagerhandler.New()
	if err != nil {
		return nil, fmt.Errorf("unable to init a window manager handler: %w", err)
	}

	return &eventSensor{
		WMH: wmh,
	}, nil
}

type submitEventer interface {
	SubmitEvent(
		ctx context.Context,
		event event.Event,
	) error
}

func (es *eventSensor) Loop(
	ctx context.Context,
	eventSubmitter submitEventer,
) {
	windowFocusChangeChan := es.WMH.WindowFocusChangeChan(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-windowFocusChangeChan:
			if err := es.submitEventWindowFocusChange(ctx, ev, eventSubmitter); err != nil {
				logger.Errorf(ctx, "unable to submit the WindowFocusChange event %#+v: %w", ev, err)
			}
		}
	}
}

func (es *eventSensor) submitEventWindowFocusChange(
	ctx context.Context,
	ev windowmanagerhandler.WindowFocusChange,
	submitEventer submitEventer,
) error {
	logger.Debugf(ctx, "submitEventWindowFocusChange(ctx, %s)", spew.Sdump(ev))
	defer logger.Debugf(ctx, "/submitEventWindowFocusChange(ctx, %#v)", spew.Sdump(ev))

	var err *multierror.Error

	if es.PreviouslyFocusedWindow != nil {
		ev := es.PreviouslyFocusedWindow
		err = multierror.Append(err, submitEventer.SubmitEvent(ctx, &event.WindowFocusChange{
			Host:        hostname,
			WindowID:    (*uint64)(ev.WindowID),
			WindowTitle: ev.WindowTitle,
			UserID:      ptr(uint64(*ev.UserID)),
			ProcessID:   ptr(uint64(*ev.ProcessID)),
			ProcessName: ev.ProcessName,
			IsFocused:   ptr(false),
		}))
	}

	es.PreviouslyFocusedWindow = &ev
	err = multierror.Append(err, submitEventer.SubmitEvent(ctx, &event.WindowFocusChange{
		Host:        hostname,
		WindowID:    (*uint64)(ev.WindowID),
		WindowTitle: ev.WindowTitle,
		UserID:      ptr(uint64(*ev.UserID)),
		ProcessID:   ptr(uint64(*ev.ProcessID)),
		ProcessName: ev.ProcessName,
		IsFocused:   ptr(true),
	}))

	return err.ErrorOrNil()
}
