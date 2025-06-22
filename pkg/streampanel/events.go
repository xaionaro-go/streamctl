package streampanel

import (
	"context"
	"fmt"
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
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

	p.eventSensor = es

	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "eventSensor")
		defer logger.Debugf(ctx, "/eventSensor")
		es.Loop(ctx, p.StreamD)
	})
}

type eventSensor struct {
	WMH        *windowmanagerhandler.WindowManagerHandler
	CancelFunc context.CancelFunc

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
	ctx, cancelFn := context.WithCancel(ctx)
	windowFocusChangeChan := es.WMH.WindowFocusChangeChan(ctx)
	if es.CancelFunc != nil {
		panic("this sensor was already used")
	}
	es.CancelFunc = cancelFn

	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-windowFocusChangeChan:
			if err := es.submitEventWindowFocusChange(ctx, ev, eventSubmitter); err != nil {
				logger.Errorf(ctx, "unable to submit the WindowFocusChange event %#+v: %v", ev, err)
			}
		}
	}
}

func (es *eventSensor) Close() error {
	var err *multierror.Error
	err = multierror.Append(err, es.WMH.Close())
	es.CancelFunc()
	return err.ErrorOrNil()
}

func (es *eventSensor) submitEventWindowFocusChange(
	ctx context.Context,
	ev windowmanagerhandler.WindowFocusChange,
	submitEventer submitEventer,
) (_err error) {
	logger.Tracef(ctx, "submitEventWindowFocusChange(ctx, %s)", spew.Sdump(ev))
	defer func() { logger.Debugf(ctx, "/submitEventWindowFocusChange(ctx, %#v): %v", spew.Sdump(ev), _err) }()

	var err *multierror.Error

	constructEvent := func(
		ev *windowmanagerhandler.WindowFocusChange,
		isFocused bool,
	) *event.WindowFocusChange {
		r := &event.WindowFocusChange{
			Host:        hostname,
			WindowID:    (*uint64)(ev.WindowID),
			WindowTitle: ev.WindowTitle,
			ProcessName: ev.ProcessName,
			IsFocused:   &isFocused,
		}
		if ev.UserID != nil {
			r.UserID = ptr(uint64(*ev.UserID))
		}
		if ev.ProcessID != nil {
			r.ProcessID = ptr(uint64(*ev.ProcessID))
		}
		return r
	}

	if es.PreviouslyFocusedWindow != nil {
		err = multierror.Append(
			err,
			submitEventer.SubmitEvent(
				ctx,
				constructEvent(es.PreviouslyFocusedWindow, false),
			),
		)
	}

	es.PreviouslyFocusedWindow = &ev
	err = multierror.Append(
		err,
		submitEventer.SubmitEvent(ctx, constructEvent(&ev, true)),
	)

	return err.ErrorOrNil()
}
