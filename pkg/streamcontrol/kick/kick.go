package kick

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/kickcom/pkg/kickcom"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type Client interface {
	ChatClient
}

type Kick struct {
	Channel     *kickcom.ChannelV1
	Client      Client
	ChatHandler *ChatHandler
}

var _ streamcontrol.StreamController[StreamProfile] = (*Kick)(nil)

func New(channelSlug string) (*Kick, error) {
	ctx := context.TODO()

	client, err := kickcom.New()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a client to Kick: %w", err)
	}

	channel, err := client.GetChannelV1(ctx, channelSlug)
	if err != nil {
		return nil, fmt.Errorf("unable to obtain channel info: %w", err)
	}

	k := &Kick{
		Client:  client,
		Channel: channel,
	}

	chatHandler, err := k.newChatHandler(ctx, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a chat handler: %w", err)
	}
	k.ChatHandler = chatHandler

	return k, nil
}

func (k *Kick) Close() error {
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
	return nil, fmt.Errorf("not implemented, yet")
}
func (k *Kick) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.ChatMessage, error) {
	logger.Debugf(ctx, "GetChatMessagesChan")
	defer logger.Debugf(ctx, "/GetChatMessagesChan")

	outCh := make(chan streamcontrol.ChatMessage)
	observability.Go(ctx, func() {
		defer func() {
			close(outCh)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-k.ChatHandler.MessagesChan():
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
