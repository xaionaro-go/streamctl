package streampanel

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/xcontext"
	xfyne "github.com/xaionaro-go/xfyne/widget"
	"github.com/xaionaro-go/xsync"
)

type timersUI struct {
	locker              xsync.Mutex
	CanvasObject        fyne.CanvasObject
	panel               *Panel
	fieldHours          *xfyne.NumericalEntry
	fieldMinutes        *xfyne.NumericalEntry
	fieldSeconds        *xfyne.NumericalEntry
	button              *widget.Button
	timer               *time.Timer
	deadline            time.Time
	timerCancelFunc     context.CancelFunc
	refresherCancelFunc context.CancelFunc
	closeChan           chan struct{}
}

func NewTimersUI(
	ctx context.Context,
	panel *Panel,
) *timersUI {
	ui := &timersUI{
		panel:        panel,
		fieldHours:   xfyne.NewNumericalEntry(),
		fieldMinutes: xfyne.NewNumericalEntry(),
		fieldSeconds: xfyne.NewNumericalEntry(),
		closeChan:    make(chan struct{}),
	}
	close(ui.closeChan) // is not started, hence already closed.

	ui.button = widget.NewButtonWithIcon(
		"Kick off the stop-stream timer",
		theme.MediaPlayIcon(), func() {
			ui.startStopButton(ctx)
		},
	)
	uiObj := container.NewVBox(
		container.NewHBox(
			widget.NewLabel("Stop stream in"),
			ui.fieldHours,
			widget.NewLabel("hours"),
			ui.fieldMinutes,
			widget.NewLabel("mins"),
			ui.fieldSeconds,
			widget.NewLabel("secs"),
		),
		ui.button,
	)
	ui.CanvasObject = uiObj
	return ui
}

func (ui *timersUI) StartRefreshingFromRemote(
	ctx context.Context,
) {
	logger.Debugf(ctx, "StartRefreshingFromRemote(ctx)")
	defer logger.Debugf(ctx, "/StartRefreshingFromRemote(ctx)")

	ui.locker.Do(ctx, func() {
		if ui.refresherCancelFunc != nil {
			ui.refresherCancelFunc()
		}
		ctx, cancelFn := context.WithCancel(ctx)
		ui.refresherCancelFunc = cancelFn

		ui.refreshFromRemote(ctx)
		observability.Go(ctx, func(ctx context.Context) {
			t := time.NewTicker(time.Second * 5)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}

				ui.RefreshFromRemote(ctx)
			}
		})
	})
}

func (ui *timersUI) RefreshFromRemote(
	ctx context.Context,
) {
	logger.Debugf(ctx, "RefreshFromRemote(ctx)")
	defer logger.Debugf(ctx, "/RefreshFromRemote(ctx)")

	ui.locker.Do(ctx, func() {
		ui.refreshFromRemote(ctx)
	})
}

func (ui *timersUI) refreshFromRemote(
	ctx context.Context,
) {
	logger.Debugf(ctx, "refreshFromRemote(ctx)")
	defer logger.Debugf(ctx, "/refreshFromRemote(ctx)")

	streamD := ui.panel.StreamD
	timers, err := streamD.ListTimers(ctx)
	if err != nil {
		ui.panel.ReportError(err)
		return
	}

	var triggerAt time.Time
	for _, timer := range timers {
		switch timer.Action.(type) {
		case *action.Noop:
			continue
		case *action.StartStream:
			continue
		case *action.EndStream:
			triggerAt = timer.TriggerAt
		default:
			continue
		}
		break
	}

	switch {
	case triggerAt.Unix() == ui.deadline.Unix():
		return
	case triggerAt.IsZero():
	default:
		logger.Debugf(ctx, "updated the deadline: %v != %v", triggerAt.Unix(), ui.deadline.Unix())
		ui.stop(ctx)
		ui.doStart(ctx, triggerAt)
	}
}

func (ui *timersUI) StopRefreshingFromRemote(
	ctx context.Context,
) {
	logger.Debugf(ctx, "StopRefreshingFromRemote(ctx)")
	defer logger.Debugf(ctx, "/StopRefreshingFromRemote(ctx)")

	ui.locker.Do(ctx, func() {
		if ui.refresherCancelFunc == nil {
			return
		}
		ui.refresherCancelFunc()
		ui.refresherCancelFunc = nil
	})
}

func (ui *timersUI) startStopButton(
	ctx context.Context,
) {
	logger.Debugf(ctx, "startStopButton(ctx)")
	defer logger.Debugf(ctx, "/startStopButton(ctx)")

	ui.locker.Do(ctx, func() {
		if ui.timer == nil {
			ui.start(ctx)
			return
		}
		ui.stop(ctx)
	})
}

func (ui *timersUI) start(
	ctx context.Context,
) {
	logger.Debugf(ctx, "start(ctx)")

	var (
		hours, mins, secs uint64
		err               error
	)

	if t := ui.fieldHours.Text; t != "" {
		hours, err = strconv.ParseUint(t, 10, 64)
		if err != nil {
			ui.panel.DisplayError(fmt.Errorf("unable to parse the hours value '%s': %w", t, err))
			return
		}
	}

	if t := ui.fieldMinutes.Text; t != "" {
		mins, err = strconv.ParseUint(t, 10, 64)
		if err != nil {
			ui.panel.DisplayError(fmt.Errorf("unable to parse the hours value '%s': %w", t, err))
			return
		}
	}

	if t := ui.fieldSeconds.Text; t != "" {
		secs, err = strconv.ParseUint(t, 10, 64)
		if err != nil {
			ui.panel.DisplayError(fmt.Errorf("unable to parse the hours value '%s': %w", t, err))
			return
		}
	}

	duration := time.Hour*time.Duration(
		hours,
	) + time.Minute*time.Duration(
		mins,
	) + time.Second*time.Duration(
		secs,
	)

	if duration == 0 {
		ui.panel.DisplayError(fmt.Errorf("the time is not set for the timer"))
		return
	}

	ui.doStart(ctx, time.Now().Add(duration))
}

func (ui *timersUI) kickOffRemotely(
	ctx context.Context,
	deadline time.Time,
) error {
	logger.Debugf(ctx, "kickOffRemotely(ctx, %v)", deadline)
	defer logger.Debugf(ctx, "/kickOffRemotely(ctx, %v)", deadline)

	if err := ui.stopRemoteTimer(ctx); err != nil {
		logger.Errorf(ctx, "unable to stop the remote timers (on cleanup): %v", err)
	}

	streamD := ui.panel.StreamD
	var result error
	for _, platID := range []streamcontrol.PlatformName{
		youtube.ID,
		twitch.ID,
		kick.ID,
		obs.ID,
	} {
		_, err := streamD.AddTimer(ctx, deadline, &action.EndStream{
			PlatID: platID,
		})
		if err != nil {
			result = fmt.Errorf("unable to start a timer for platform '%s': %w", platID, err)
			break
		}
	}

	if result == nil {
		return nil
	}

	if err := ui.stopRemoteTimer(ctx); err != nil {
		logger.Errorf(ctx, "unable to stop the remote timers (on cleanup): %v", err)
	}

	return result
}

func (ui *timersUI) doStart(
	ctx context.Context,
	deadline time.Time,
) {
	logger.Debugf(ctx, "doStart(ctx, %v)", deadline)
	defer logger.Debugf(ctx, "/doStart(ctx, %v)", deadline)

	err := ui.kickOff(ctx, deadline)
	if err != nil {
		ui.panel.DisplayError(err)
		return
	}

	ui.closeChan = make(chan struct{})
	ui.button.SetText("Stop the stop-stream timer")
	ui.button.Importance = widget.DangerImportance
	ui.button.Icon = theme.MediaPauseIcon()
	ui.button.Refresh()
}

func (ui *timersUI) kickOff(
	ctx context.Context,
	deadline time.Time,
) error {
	logger.Debugf(ctx, "kickOff(ctx, %v)", deadline)
	defer logger.Debugf(ctx, "/kickOff(ctx, %v)", deadline)

	err := ui.kickOffRemotely(ctx, deadline)
	if err != nil {
		return fmt.Errorf("unable to start the timer on the remote side: %w", err)
	}

	ui.deadline = deadline
	ctx, ui.timerCancelFunc = context.WithCancel(ctx)
	ui.timer = time.NewTimer(time.Until(deadline))

	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			ui.doStop(xcontext.DetachDone(ctx))
		}()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				ui.UpdateFields(ctx)
			case <-ui.timer.C:
				return
			}
		}
	})
	return nil
}

func (ui *timersUI) UpdateFields(
	ctx context.Context,
) {
	ui.locker.Do(ctx, func() {
		ui.updateFields(ctx)
	})
}

func (ui *timersUI) updateFields(
	ctx context.Context,
) {
	timeLeft := time.Until(ui.deadline) + time.Second/2
	if timeLeft < 0 {
		timeLeft = 0
	}
	logger.Debugf(ctx, "updateFields: %v", timeLeft)

	hours := uint(timeLeft.Hours())
	timeLeft -= time.Duration(hours) * time.Hour
	mins := uint(timeLeft.Minutes())
	timeLeft -= time.Duration(mins) * time.Minute
	secs := uint(timeLeft.Seconds())

	logger.Debugf(ctx, "updateFields: %dh%dm%ds", hours, mins, secs)

	if hours == 0 {
		ui.fieldHours.SetText("")
	} else {
		ui.fieldHours.SetText(fmt.Sprintf("%d", hours))
	}

	if mins == 0 {
		ui.fieldMinutes.SetText("")
	} else {
		ui.fieldMinutes.SetText(fmt.Sprintf("%d", mins))
	}

	if secs == 0 {
		ui.fieldSeconds.SetText("")
	} else {
		ui.fieldSeconds.SetText(fmt.Sprintf("%d", secs))
	}
}

func (ui *timersUI) stopRemoteTimer(
	ctx context.Context,
) error {
	streamD := ui.panel.StreamD
	timers, err := streamD.ListTimers(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the list of current timers: %w", err)
	}

	var result *multierror.Error
	for _, timer := range timers {
		err := streamD.RemoveTimer(ctx, timer.ID)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				logger.Warnf(ctx, "unable to delete timer %d: %v", timer.ID, err)
			} else {
				result = multierror.Append(result, fmt.Errorf("unable to delete remote timer %d", err))
			}
		}
	}

	return result.ErrorOrNil()
}

func (ui *timersUI) doStop(
	ctx context.Context,
) {
	logger.Debugf(ctx, "doStop(ctx)")
	defer logger.Debugf(ctx, "/doStop(ctx)")
	ui.locker.Do(ctx, func() {
		select {
		case <-ui.closeChan:
			logger.Debugf(ctx, "already closed")
			return // already closed
		default:
		}

		ui.updateFields(ctx)
		ui.timer.Stop()
		ui.timer = nil
		ui.button.SetText("Kick off the stop-stream timer")
		ui.button.Importance = widget.MediumImportance
		ui.button.Icon = theme.MediaPlayIcon()
		ui.button.Refresh()
		ui.deadline = time.Time{}
		ui.timerCancelFunc = nil
		closeChan := ui.closeChan
		ui.closeChan = make(chan struct{})
		close(closeChan)

		err := ui.stopRemoteTimer(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to stop the remote timers: %v", err)
		}
	})
}

func (ui *timersUI) stop(
	ctx context.Context,
) {
	logger.Debugf(ctx, "stop")
	if ui.timerCancelFunc == nil {
		return
	}
	closeChan := ui.closeChan
	ui.timerCancelFunc()
	ui.locker.UDo(ctx, func() {
		<-closeChan
	})
}
