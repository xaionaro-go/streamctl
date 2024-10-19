package streamd

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func (d *StreamD) startListeningForChatMessages(
	ctx context.Context,
	platName streamcontrol.PlatformName,
) error {
	ctrl, err := d.streamController(ctx, platName)
	if err != nil {
		return fmt.Errorf("unable to get the just initialized '%s': %w", platName, err)
	}
	ch, err := ctrl.GetChatMessagesChan(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the channel for chat messages of '%s': %w", platName, err)
	}
	observability.Go(ctx, func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				d.publishEvent(ctx, api.ChatMessage{
					ChatMessage: ev,
					Platform:    platName,
				})
			}
		}
	})
	return nil
}
