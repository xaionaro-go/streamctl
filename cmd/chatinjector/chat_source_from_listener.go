package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ChatSourceFromListener bridges a chathandler.ChatListener into the
// chatinjector's ChatSource interface. It starts the listener, forwards
// events into the shared ChatEvent channel, and closes the listener
// when the context is cancelled or the event channel is drained.
type ChatSourceFromListener struct {
	Listener     chathandler.ChatListener
	PlatformName streamcontrol.PlatformName
}

func (s *ChatSourceFromListener) PlatformID() streamcontrol.PlatformName {
	return s.PlatformName
}

func (s *ChatSourceFromListener) Run(
	ctx context.Context,
	events chan<- ChatEvent,
) (_err error) {
	logger.Tracef(ctx, "ChatSourceFromListener[%s].Run", s.Listener.Name())
	defer func() { logger.Tracef(ctx, "/ChatSourceFromListener[%s].Run: %v", s.Listener.Name(), _err) }()

	evCh, err := s.Listener.Listen(ctx)
	if err != nil {
		return fmt.Errorf("listener %s: %w", s.Listener.Name(), err)
	}
	defer func() {
		if closeErr := s.Listener.Close(ctx); closeErr != nil {
			logger.Warnf(ctx, "close listener %s: %v", s.Listener.Name(), closeErr)
		}
	}()

	logger.Debugf(ctx, "listener %s started", s.Listener.Name())

	for ev := range evCh {
		logger.Debugf(ctx, "received %s event: id=%s type=%s user=%s msg=%q",
			s.Listener.Name(), ev.ID, ev.Type, ev.User.Name, messageContent(ev))

		select {
		case events <- ChatEvent{Event: ev, Platform: s.PlatformName}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}
