package streampanel

import (
	"context"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type streamStatus struct {
	LastChangedAt    time.Time
	BackendIsEnabled bool
	BackendError     error
	streamcontrol.StreamStatus
}

func (p *Panel) updateStreamStatus(
	ctx context.Context,
) {
	logger.Debugf(ctx, "updateStreamStatus")
	defer logger.Debugf(ctx, "/updateStreamStatus")
	ctx, cancelFn := context.WithTimeout(ctx, 30*time.Second)
	defer cancelFn()

	var wg sync.WaitGroup
	for _, platID := range []streamcontrol.PlatformName{
		obs.ID,
		youtube.ID,
		twitch.ID,
		kick.ID,
	} {
		wg.Add(1)
		observability.Go(ctx, func() {
			defer wg.Done()

			ok, err := p.StreamD.IsBackendEnabled(ctx, platID)
			if err != nil {
				logger.Error(ctx, err)
				p.setStreamStatus(ctx, platID, streamStatus{
					BackendError: err,
				})
				return
			}

			if !ok {
				p.streamStatusLocker.Do(ctx, func() {
					p.setStreamStatus(ctx, platID, streamStatus{})
				})
				return
			}

			status, err := p.StreamD.GetStreamStatus(ctx, platID)
			if err != nil {
				logger.Error(ctx, err)
				p.setStreamStatus(ctx, platID, streamStatus{
					BackendIsEnabled: true,
					BackendError:     err,
				})
				return
			}

			p.setStreamStatus(ctx, platID, streamStatus{
				StreamStatus:     *status,
				BackendIsEnabled: true,
			})
		})
	}

	wg.Wait()
}

func (p *Panel) setStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	newStatus streamStatus,
) {
	p.streamStatusLocker.Do(ctx, func() {
		oldStatus := p.streamStatus[platID]
		isSignificantlyDifferent := false
		if oldStatus == nil {
			oldStatus = &streamStatus{}
			isSignificantlyDifferent = true
		}
		if oldStatus.BackendIsEnabled != newStatus.BackendIsEnabled {
			isSignificantlyDifferent = true
		}
		if (oldStatus.StartedAt != nil) != (newStatus.StartedAt != nil) {
			isSignificantlyDifferent = true
		}
		if (oldStatus.BackendError != nil) != (newStatus.BackendError != nil) {
			isSignificantlyDifferent = true
		}
		if oldStatus.IsActive != newStatus.IsActive {
			isSignificantlyDifferent = true
		}
		if isSignificantlyDifferent {
			newStatus.LastChangedAt = time.Now()
		} else {
			newStatus.LastChangedAt = oldStatus.LastChangedAt
		}
		p.streamStatus[platID] = &newStatus
	})
}
