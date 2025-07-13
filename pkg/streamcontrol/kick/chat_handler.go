package kick

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/scorfly/gokick"
)

type ChatHandler struct {
	onClose func(context.Context)
}

func (k *Kick) newChatHandler(
	ctx context.Context,
	broadcasterUserID *int,
	onClose func(context.Context),
) (*ChatHandler, error) {
	return NewChatHandler(ctx, k.Client, broadcasterUserID, onClose)
}

func NewChatHandler(
	ctx context.Context,
	client *gokick.Client,
	broadcasterUserID *int,
	onClose func(context.Context),
) (_ret *ChatHandler, _err error) {
	logger.Debugf(ctx, "NewChatHandler(ctx, client, %p)", onClose)
	defer func() {
		logger.Debugf(ctx, "/NewChatHandler(ctx, client, %p): %#+v %v", onClose, _ret, _err)
	}()

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	subscriptions := []gokick.SubscriptionRequest{
		{
			Name:    gokick.SubscriptionNameChatMessage,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameChannelFollow,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameChannelSubscriptionRenewal,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameChannelSubscriptionGifts,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameChannelSubscriptionCreated,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameLivestreamStatusUpdated,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameLivestreamMetadataUpdated,
			Version: 1,
		},
		{
			Name:    gokick.SubscriptionNameModerationBanned,
			Version: 1,
		},
	}
	response, err := client.CreateSubscriptions(
		ctx,
		gokick.SubscriptionMethodWebhook, // PANIC: we need websockets, we cannot use webhook
		subscriptions,
		broadcasterUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create the event subscriptions: %w", err)
	}
	logger.Debugf(ctx, "subscriptions: %#+v", response)

	panic("not implemented, yet")
}
