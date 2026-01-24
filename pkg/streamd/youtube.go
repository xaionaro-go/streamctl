package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	upstreamyoutube "google.golang.org/api/youtube/v3"
)

func (d *StreamD) updateYoutubeBroadcasts(
	ctx context.Context,
	accountID streamcontrol.AccountID,
) (bool, error) {
	oldCount := len(d.GetYouTubeCache(ctx, accountID).Broadcasts)
	logger.FromCtx(ctx).Debugf("downloading new broadcasts only for account '%s' (count: %d)", accountID, oldCount)
	defer func() {
		logger.FromCtx(ctx).Debugf("downloading new broadcasts only for account '%s' (count: %d -> %d)", accountID, oldCount, len(d.GetYouTubeCache(ctx, accountID).Broadcasts))
	}()

	var anyYoutube *youtube.YouTube
	controllers := d.getControllersByPlatform(youtube.ID)
	c, ok := controllers[accountID]
	if !ok {
		return false, fmt.Errorf("youtube controller for account '%s' is not initialized", accountID)
	}
	anyYoutube = c.GetImplementation().(*youtube.YouTube)

	broadcastIDs := map[string]struct{}{}
	d.CacheLock.Do(ctx, func() {
		for _, broadcast := range d.getYouTubeCache(ctx, accountID).Broadcasts {
			broadcastIDs[broadcast.Id] = struct{}{}
		}
	})

	broadcasts, err := anyYoutube.ListBroadcasts(
		d.ctxForController(ctx),
		50000,
		func(resp *upstreamyoutube.LiveBroadcastListResponse) bool {
			for _, broadcast := range resp.Items {
				if _, ok := broadcastIDs[broadcast.Id]; ok {
					return false
				}
			}
			return true
		},
	)
	if err != nil {
		return false, fmt.Errorf("unable to get new broadcasts: %w", err)
	}

	if len(broadcasts) == 0 {
		return false, nil
	}

	var (
		prevLen, newLen int
	)
	d.CacheLock.Do(ctx, func() {
		cache := d.getYouTubeCache(ctx, accountID)
		prevLen = len(cache.Broadcasts)
		expectedLen := len(broadcasts) + len(cache.Broadcasts)
		result := make([]*upstreamyoutube.LiveBroadcast, 0, expectedLen)
		alreadySet := make(map[string]struct{}, expectedLen)
		appendWith := func(broadcasts []*upstreamyoutube.LiveBroadcast) {
			for _, broadcast := range broadcasts {
				if _, ok := alreadySet[broadcast.Id]; ok {
					continue
				}
				alreadySet[broadcast.Id] = struct{}{}
				result = append(result, broadcast)
			}
		}
		appendWith(broadcasts)
		appendWith(cache.Broadcasts)
		cache.Broadcasts = result
		newLen = len(result)
	})
	return newLen != prevLen, nil
}
