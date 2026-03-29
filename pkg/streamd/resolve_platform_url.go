package streamd

import (
	"context"
	"fmt"
	"net/url"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) ResolvePlatformURL(
	ctx context.Context,
	rawURL string,
) (_ streamcontrol.PlatformID, _ streamcontrol.StreamID, _err error) {
	logger.Debugf(ctx, "ResolvePlatformURL(ctx, '%s')", rawURL)
	defer func() { logger.Debugf(ctx, "/ResolvePlatformURL(ctx, '%s'): %v", rawURL, _err) }()

	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("unable to parse URL '%s': %w", rawURL, err)
	}

	platID, streamID, ok := xsync.DoR3(ctx, &d.AccountsLocker, func() (streamcontrol.PlatformID, streamcontrol.StreamID, bool) {
		for _, account := range d.AccountMap {
			if !account.IsChannelURL(u) {
				continue
			}
			streamID, err := account.ExtractStreamID(u)
			if err != nil {
				logger.Errorf(ctx, "unable to extract stream ID from URL '%s' for platform '%s': %v", rawURL, account.GetPlatformID(), err)
				continue
			}
			return account.GetPlatformID(), streamID, true
		}
		return "", "", false
	})
	if ok {
		return platID, streamID, nil
	}

	return "", "", fmt.Errorf("no platform recognized URL '%s'", rawURL)
}
