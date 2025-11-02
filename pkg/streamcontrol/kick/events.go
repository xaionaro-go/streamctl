package kick

import (
	"context"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/scorfly/gokick"
	"github.com/xaionaro-go/chatwebhook/pkg/chatwebhook/kickcom/events"
)

func (k *Kick) subscribeToEvents(ctx context.Context) {
	var subReqs []gokick.SubscriptionRequest
	for _, eventSample := range events.All() {
		subReqs = append(subReqs, gokick.SubscriptionRequest{
			Name:    must(gokick.NewSubscriptionName(eventSample.TypeName())),
			Version: eventSample.Version(),
		})
	}
	for {
		c := k.GetClient()
		resp, err := c.CreateSubscriptions(
			ctx,
			gokick.SubscriptionMethodWebhook,
			subReqs,
			ptr(int(k.Channel.ID)),
		)
		logger.Debugf(ctx, "Kick subscriptions results: %+v, err: %v", resp, err)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
}
