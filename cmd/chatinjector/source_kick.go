package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	chatwebhookclient "github.com/xaionaro-go/chatwebhook/pkg/grpc/client"
	"github.com/xaionaro-go/chatwebhook/pkg/grpc/protobuf/go/chatwebhook_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kick "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
	scgoconv "github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
)

// KickSource implements ChatSource by subscribing to a chatwebhook gRPC
// service that relays Kick chat events.
type KickSource struct {
	// ChatWebhookAddr is the address of the chatwebhook gRPC server.
	// Defaults to chatwebhookclient.DefaultServerAddress if empty.
	ChatWebhookAddr string
}

func (s *KickSource) PlatformID() streamcontrol.PlatformName {
	return kick.ID
}

// Run connects to the chatwebhook gRPC service, subscribes to Kick chat
// events, and emits ChatEvents until the context is cancelled or the
// event stream closes.
func (s *KickSource) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "KickSource.Run")
	defer func() { logger.Tracef(ctx, "/KickSource.Run: %v", _err) }()

	addr := s.ChatWebhookAddr
	if addr == "" {
		addr = chatwebhookclient.DefaultServerAddress
	}

	client, err := chatwebhookclient.New(ctx, addr)
	if err != nil {
		return fmt.Errorf("create chatwebhook client at %q: %w", addr, err)
	}

	logger.Debugf(ctx, "subscribing to Kick chat via chatwebhook at %s", addr)

	msgCh, err := client.GetMessagesChan(
		ctx,
		chatwebhook_grpc.PlatformID_platformIDKick,
		"",
	)
	if err != nil {
		return fmt.Errorf("subscribe to Kick chat messages: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case grpcEv, ok := <-msgCh:
			if !ok {
				logger.Debugf(ctx, "Kick chat message channel closed")
				return nil
			}
			if grpcEv == nil {
				logger.Warnf(ctx, "received nil Kick event, skipping")
				continue
			}

			ev := scgoconv.EventGRPC2Go(grpcEv)
			logger.Debugf(ctx, "received kick event: id=%s type=%s user=%s msg=%q",
				ev.ID, ev.Type, ev.User.Name, messageContent(ev))

			select {
			case events <- ChatEvent{Event: ev, Platform: kick.ID}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
