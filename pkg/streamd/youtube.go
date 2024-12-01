package streamd

import (
	"context"
	"fmt"
	"sort"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	upstreamyoutube "google.golang.org/api/youtube/v3"
)

func (d *StreamD) initYoutubeData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Youtube data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Youtube data")

	if c := len(d.Cache.Youtube.Broadcasts); c != 0 {
		logger.FromCtx(ctx).Debugf("already have broadcasts (count: %d)", c)
		updated, err := d.updateYoutubeBroadcasts(ctx)
		if err != nil {
			d.UI.DisplayError(err)
		}
		return updated
	}

	youtube := d.StreamControllers.YouTube
	if youtube == nil {
		logger.FromCtx(ctx).Debugf("youtube controller is not initialized")
		return false
	}

	broadcasts, err := youtube.ListBroadcasts(d.ctxForController(ctx), 50000, nil)
	if err != nil {
		d.UI.DisplayError(err)
		return false
	}

	logger.FromCtx(ctx).Debugf("got broadcasts: %#+v", broadcasts)
	d.CacheLock.Do(ctx, func() {
		d.Cache.Youtube.Broadcasts = broadcasts
	})

	err = d.SaveConfig(ctx)
	errmon.ObserveErrorCtx(ctx, err)
	return true
}

func (d *StreamD) updateYoutubeBroadcasts(
	ctx context.Context,
) (bool, error) {
	oldCount := len(d.Cache.Youtube.Broadcasts)
	logger.FromCtx(ctx).Debugf("downloading new broadcasts only (count: %d)", oldCount)
	defer func() {
		logger.FromCtx(ctx).Debugf("downloading new broadcasts only (count: %d -> %d)", oldCount, len(d.Cache.Youtube.Broadcasts))
	}()

	yt := d.StreamControllers.YouTube
	if yt == nil {
		return false, fmt.Errorf("youtube controller is not initialized")
	}

	broadcastIDs := map[string]struct{}{}
	d.CacheLock.Do(ctx, func() {
		for _, broadcast := range d.Cache.Youtube.Broadcasts {
			broadcastIDs[broadcast.Id] = struct{}{}
		}
	})

	broadcasts, err := yt.ListBroadcasts(
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
		prevLen = len(d.Cache.Youtube.Broadcasts)
		expectedLen := len(broadcasts) + len(d.Cache.Youtube.Broadcasts)
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
		appendWith(d.Cache.Youtube.Broadcasts)
		d.Cache.Youtube.Broadcasts = result
		newLen = len(result)
	})
	return newLen != prevLen, nil
}

func (d *StreamD) normalizeYoutubeData() {
	s := d.Cache.Youtube.Broadcasts
	sort.Slice(s, func(i, j int) bool {
		return s[i].Snippet.Title < s[j].Snippet.Title
	})
}
