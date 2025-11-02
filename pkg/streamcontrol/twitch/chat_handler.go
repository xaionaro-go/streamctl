package twitch

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatHandler interface {
	Close(ctx context.Context) error
	MessagesChan() <-chan streamcontrol.Event
}
