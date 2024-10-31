package kick

import (
	"context"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/kickcom"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type Client interface {
	ChatClient
	LivestreamInfoClient
}

type LivestreamInfoClient interface {
	GetLivestreamV2(
		ctx context.Context,
		channelSlug string,
	) (*kickcom.LivestreamV2Reply, error)
}

type Kick struct {
	CloseCtx    context.Context
	CloseFn     context.CancelFunc
	Channel     *kickcom.ChannelV1
	Client      Client
	ChatHandler *ChatHandler
	SaveCfgFn   func(Config) error
}

var _ streamcontrol.StreamController[StreamProfile] = (*Kick)(nil)

func New(
	ctx context.Context,
	cfg Config,
	saveCfgFn func(Config) error,
) (*Kick, error) {

	if cfg.Config.Channel == "" {
		return nil, fmt.Errorf("channel is not set")
	}

	var err error
	var client *kickcom.Kick
	var channel *kickcom.ChannelV1
	for i := 0; i < 10; i++ {

		client, err = kickcom.New()
		if err != nil {
			err = fmt.Errorf("unable to initialize a client to Kick: %w", err)
			time.Sleep(time.Second)
			continue
		}

		channel, err = client.GetChannelV1(ctx, cfg.Config.Channel)
		if err != nil {
			err = fmt.Errorf("unable to obtain channel info: %w", err)
			time.Sleep(time.Second)
			continue
		}
	}
	if err != nil {
		return nil, err
	}

	ctx, closeFn := context.WithCancel(ctx)
	k := &Kick{
		CloseCtx:  ctx,
		CloseFn:   closeFn,
		Client:    client,
		Channel:   channel,
		SaveCfgFn: saveCfgFn,
	}

	chatHandler, err := k.newChatHandler(ctx, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a chat handler: %w", err)
	}
	k.ChatHandler = chatHandler

	return k, nil
}

func (k *Kick) Close() error {
	k.CloseFn()
	return nil
}
func (k *Kick) SetTitle(ctx context.Context, title string) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) SetDescription(ctx context.Context, description string) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) InsertAdsCuePoint(ctx context.Context, ts time.Time, duration time.Duration) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) Flush(ctx context.Context) error {
	return nil
}
func (k *Kick) EndStream(ctx context.Context) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) GetStreamStatus(ctx context.Context) (*streamcontrol.StreamStatus, error) {
	info, err := k.Client.GetLivestreamV2(ctx, k.Channel.Slug)
	if err != nil {
		return nil, fmt.Errorf("unable to request stream status: %w", err)
	}
	logger.Tracef(ctx, "the received livestream status is: %s", spew.Sdump(info))

	if info.Data == nil {
		return &streamcontrol.StreamStatus{
			IsActive:     false,
			ViewersCount: nil,
			StartedAt:    nil,
			CustomData:   nil,
		}, nil
	}

	return &streamcontrol.StreamStatus{
		IsActive:     true,
		ViewersCount: ptr(uint(info.Data.Viewers)),
		StartedAt:    &info.Data.CreatedAt,
		CustomData:   nil,
	}, nil
}
func (k *Kick) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.ChatMessage, error) {
	logger.Debugf(ctx, "GetChatMessagesChan")
	defer logger.Debugf(ctx, "/GetChatMessagesChan")

	outCh := make(chan streamcontrol.ChatMessage)
	observability.Go(ctx, func() {
		defer func() {
			logger.Debugf(ctx, "closing the messages channel")
			close(outCh)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-k.ChatHandler.MessagesChan():
				if !ok {
					logger.Debugf(ctx, "the input channel is closed")
					return
				}
				outCh <- ev
			}
		}
	})

	return outCh, nil
}
func (k *Kick) SendChatMessage(ctx context.Context, message string) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) RemoveChatMessage(ctx context.Context, messageID streamcontrol.ChatMessageID) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) BanUser(
	ctx context.Context,
	userID streamcontrol.ChatUserID,
	reason string,
	deadline time.Time,
) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}
func (k *Kick) ApplyProfile(ctx context.Context, profile StreamProfile, customArgs ...any) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil

}
func (k *Kick) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) error {
	logger.Warnf(ctx, "not implemented yet")
	return nil
}

func (k *Kick) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	switch cap {
	case streamcontrol.CapabilitySendChatMessage:
		return false
	case streamcontrol.CapabilityDeleteChatMessage:
		return false
	case streamcontrol.CapabilityBanUser:
		return false
	}
	return false
}
