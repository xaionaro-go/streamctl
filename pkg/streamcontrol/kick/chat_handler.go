package kick

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/chatwebhook/pkg/chatwebhook/kickcom"
	chatwebhookclient "github.com/xaionaro-go/chatwebhook/pkg/grpc/client"
	"github.com/xaionaro-go/chatwebhook/pkg/grpc/protobuf/go/chatwebhook_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
)

type ChatHandlerAbstract interface {
	GetMessagesChan(
		ctx context.Context,
	) (<-chan streamcontrol.Event, error)
}

type ChatHandler struct {
	Client *chatwebhookclient.Client
}

var _ ChatHandlerAbstract = (*ChatHandler)(nil)

func NewChatHandler(
	ctx context.Context,
	client *chatwebhookclient.Client,
) (_ret *ChatHandler, _err error) {
	logger.Debugf(ctx, "NewChatHandler")
	defer func() {
		logger.Debugf(ctx, "/NewChatHandler: %#+v %v", _ret, _err)
	}()

	return &ChatHandler{
		Client: client,
	}, nil
}

func (h *ChatHandler) GetMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	inCh, err := h.Client.GetMessagesChan(ctx, kickcom.ID, "")
	if err != nil {
		return nil, fmt.Errorf("kick: failed to get messages chan: %w", err)
	}

	outCh := make(chan streamcontrol.Event, 1)
	observability.Go(ctx, func(ctx context.Context) {
		defer close(outCh)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-inCh:
				if !ok {
					return
				}
				msg, err := convertKickEventToChatMessage(ev)
				if err != nil {
					logger.Errorf(ctx, "failed to convert kick event to chat message: %v", err)
					continue
				}
				outCh <- msg
			}
		}
	})

	return outCh, nil
}

func convertKickEventToChatMessage(
	ev *chatwebhook_grpc.Event,
) (streamcontrol.Event, error) {
	if ev == nil {
		return streamcontrol.Event{}, fmt.Errorf("event is nil")
	}

	return goconv.EventGRPC2Go(ev), nil
}
