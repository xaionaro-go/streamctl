package kickclientobsolete

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/xsync"
)

type KickClientOBSOLETE struct {
	Client *kickcom.Kick
	xsync.Mutex
}

func New() (*KickClientOBSOLETE, error) {
	client, err := kickcom.New()
	if err != nil {
		return nil, err
	}
	return &KickClientOBSOLETE{Client: client}, nil
}

func (c *KickClientOBSOLETE) GetLivestreamV2(
	ctx context.Context,
	channelSlug string,
) (*kickcom.LivestreamV2Reply, error) {
	return kickClientOBSOLETEWrapCallA1(ctx, c, c.Client.GetLivestreamV2, channelSlug)
}

func (c *KickClientOBSOLETE) GetSubcategoriesV1(
	ctx context.Context,
) (*kickcom.CategoriesV1Reply, error) {
	return kickClientOBSOLETEWrapCallA0(ctx, c, c.Client.GetSubcategoriesV1)
}

func (c *KickClientOBSOLETE) GetChannelV1(
	ctx context.Context,
	channel string,
) (*kickcom.ChannelV1, error) {
	return kickClientOBSOLETEWrapCallA1(ctx, c, c.Client.GetChannelV1, channel)
}

func (c *KickClientOBSOLETE) GetChatMessagesV2(
	ctx context.Context,
	channelID uint64,
	cursor uint64,
) (*kickcom.ChatMessagesV2Reply, error) {
	return kickClientOBSOLETEWrapCallA2(ctx, c, c.Client.GetChatMessagesV2, channelID, cursor)
}

func kickClientOBSOLETEWrapCallA0[R0 any](
	ctx context.Context,
	c *KickClientOBSOLETE,
	fn func(context.Context) (R0, error),
) (R0, error) {
	return xsync.DoR2(ctx, &c.Mutex, func() (R0, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for {
			r0, err := fn(ctx)
			if err == nil {
				return r0, nil
			}
			err = c.fixError(ctx, err)
			if err == nil {
				continue
			}
			return r0, err
		}
	})
}

func kickClientOBSOLETEWrapCallA1[A0, R0 any](
	ctx context.Context,
	c *KickClientOBSOLETE,
	fn func(context.Context, A0) (R0, error),
	a0 A0,
) (R0, error) {
	return xsync.DoR2(ctx, &c.Mutex, func() (R0, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for {
			r0, err := fn(ctx, a0)
			if err == nil {
				return r0, nil
			}
			err = c.fixError(ctx, err)
			if err == nil {
				continue
			}
			return r0, err
		}
	})
}

func kickClientOBSOLETEWrapCallA2[A0, A1, R0 any](
	ctx context.Context,
	c *KickClientOBSOLETE,
	fn func(context.Context, A0, A1) (R0, error),
	a0 A0,
	a1 A1,
) (R0, error) {
	return xsync.DoR2(ctx, &c.Mutex, func() (R0, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for {
			r0, err := fn(ctx, a0, a1)
			if err == nil {
				return r0, nil
			}
			err = c.fixError(ctx, err)
			if err == nil {
				continue
			}
			return r0, err
		}
	})
}

func (c *KickClientOBSOLETE) fixError(ctx context.Context, err error) error {
	switch {
	case strings.Contains(err.Error(), "use of closed network connection"):
		logger.Warnf(ctx, "kick client connection is closed: %v", err)
		client, err := kickcom.New()
		if err != nil {
			return fmt.Errorf("unable to reinitialize kick client: %w", err)
		}
		logger.Debugf(ctx, "kick client reinitialized")
		c.Client = client
		return nil
	default:
		return err
	}
}
