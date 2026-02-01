package streamd

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) GetPlatforms(ctx context.Context) []streamcontrol.PlatformID {
	var platforms []streamcontrol.PlatformID
	for platID := range d.Config.Backends {
		platforms = append(platforms, platID)
	}
	return platforms
}

func (d *StreamD) GetAccounts(
	ctx context.Context,
	platformIDs ...streamcontrol.PlatformID,
) ([]streamcontrol.AccountIDFullyQualified, error) {
	if len(platformIDs) == 0 {
		platformIDs = d.GetPlatforms(ctx)
	}

	var result []streamcontrol.AccountIDFullyQualified
	for _, platID := range platformIDs {
		platCfg := d.Config.Backends[platID]
		if platCfg == nil {
			continue
		}
		for accountID := range platCfg.Accounts {
			if accountID == "" {
				continue
			}
			result = append(result, streamcontrol.NewAccountIDFullyQualified(platID, accountID))
		}
	}
	return result, nil
}

func (d *StreamD) GetStreams(
	ctx context.Context,
	accountIDs ...streamcontrol.AccountIDFullyQualified,
) ([]streamcontrol.StreamInfo, error) {
	if len(accountIDs) == 0 {
		var err error
		accountIDs, err = d.GetAccounts(ctx)
		if err != nil {
			return nil, err
		}
	}

	return xsync.RDoR2(ctx, &d.AccountsLocker, func() ([]streamcontrol.StreamInfo, error) {
		var result []streamcontrol.StreamInfo
		for _, accountID := range accountIDs {
			controllers := d.getControllersByPlatformNoLock(accountID.PlatformID)
			c, ok := controllers[accountID.AccountID]
			if !ok {
				continue
			}
			streams, err := c.GetStreams(ctx)
			if err != nil {
				continue
			}
			result = append(result, streams...)
		}
		return result, nil
	})
}

func (d *StreamD) CreateStream(
	ctx context.Context,
	accountID streamcontrol.AccountIDFullyQualified,
	title string,
) (streamcontrol.StreamInfo, error) {
	return xsync.RDoR2(ctx, &d.AccountsLocker, func() (streamcontrol.StreamInfo, error) {
		controllers := d.getControllersByPlatformNoLock(accountID.PlatformID)
		c, ok := controllers[accountID.AccountID]
		if !ok {
			return streamcontrol.StreamInfo{}, fmt.Errorf("account not found")
		}
		return c.CreateStream(ctx, title)
	})
}

func (d *StreamD) DeleteStream(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) error {
	return xsync.RDoR1(ctx, &d.AccountsLocker, func() error {
		controllers := d.getControllersByPlatformNoLock(streamID.PlatformID)
		c, ok := controllers[streamID.AccountID]
		if !ok {
			return fmt.Errorf("account not found")
		}
		return c.DeleteStream(ctx, streamID.StreamID)
	})
}

func (d *StreamD) GetActiveStreamIDs(
	ctx context.Context,
) ([]streamcontrol.StreamIDFullyQualified, error) {

	return xsync.RDoR2(ctx, &d.AccountsLocker, func() ([]streamcontrol.StreamIDFullyQualified, error) {
		var result []streamcontrol.StreamIDFullyQualified

		selectedStreamIDs := make(map[streamcontrol.StreamIDFullyQualified]struct{})
		for _, id := range d.Config.SelectedStreamIDs {
			selectedStreamIDs[id] = struct{}{}
		}

		for platID, platCfg := range d.Config.Backends {
			controllers := d.getControllersByPlatformNoLock(platID)
			for accountID, c := range controllers {
				if _, ok := platCfg.Accounts[accountID]; !ok {
					continue
				}

				streams, err := c.GetStreams(ctx)
				if err != nil {
					continue
				}

				for _, stream := range streams {
					id := streamcontrol.NewStreamIDFullyQualified(platID, accountID, stream.ID)
					if _, ok := selectedStreamIDs[id]; !ok {
						continue
					}
					result = append(result, id)
				}
			}
		}

		return result, nil
	})
}

func (d *StreamD) GetStreamSinkConfig(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (sstypes.StreamSinkConfig, error) {

	return xsync.RDoR2(ctx, &d.AccountsLocker, func() (sstypes.StreamSinkConfig, error) {
		controllers := d.getControllersByPlatformNoLock(streamID.PlatformID)
		c, ok := controllers[streamID.AccountID]
		if !ok {
			return sstypes.StreamSinkConfig{}, fmt.Errorf("account not found: %s", streamID.AccountIDFullyQualified)
		}

		status, err := c.GetStreamStatus(ctx, streamID.StreamID)
		if err != nil {
			return sstypes.StreamSinkConfig{}, fmt.Errorf("unable to get stream status: %w", err)
		}

		switch streamID.PlatformID {
		case youtube.ID:
			data := youtube.GetStreamStatusCustomData(status)
			for _, s := range data.Streams {
				if s.Snippet.Title == string(streamID.StreamID) || s.Id == string(streamID.StreamID) {
					if s.Cdn == nil || s.Cdn.IngestionInfo == nil {
						continue
					}
					return sstypes.StreamSinkConfig{
						URL:       s.Cdn.IngestionInfo.IngestionAddress,
						StreamKey: secret.New(s.Cdn.IngestionInfo.StreamName),
					}, nil
				}
			}
			if streamID.StreamID == "" && len(data.Streams) > 0 {
				s := data.Streams[0]
				if s.Cdn != nil && s.Cdn.IngestionInfo != nil {
					return sstypes.StreamSinkConfig{
						URL:       s.Cdn.IngestionInfo.IngestionAddress,
						StreamKey: secret.New(s.Cdn.IngestionInfo.StreamName),
					}, nil
				}
			}
		case twitch.ID:
			t := c.GetImplementation().(*twitch.Twitch)
			streamKey, err := t.GetStreamKey(ctx)
			if err != nil {
				return sstypes.StreamSinkConfig{}, fmt.Errorf("unable to get stream key: %w", err)
			}
			return sstypes.StreamSinkConfig{
				URL:       "rtmp://live.twitch.tv/app/",
				StreamKey: streamKey,
			}, nil
		case kick.ID:
			data := kick.GetStreamStatusCustomData(status)
			return sstypes.StreamSinkConfig{
				URL:       data.URL,
				StreamKey: data.Key,
			}, nil
		}

		return sstypes.StreamSinkConfig{}, fmt.Errorf("dynamic sink config retrieval not fully implemented for platform %s (status: %#+v)", streamID.PlatformID, status)
	})
}
