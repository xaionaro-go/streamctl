package twitch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/adeithe/go-twitch/irc"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatClientIRC interface {
	Join(channelIDs ...string) error
	OnShardMessage(func(shard int, msg irc.ChatMessage))
	Close(context.Context) error
}

type ChatHandlerIRC struct {
	client          ChatClientIRC
	cancelFunc      context.CancelFunc
	waitGroup       sync.WaitGroup
	messagesInChan  chan irc.ChatMessage
	messagesOutChan chan streamcontrol.ChatMessage
}

var _ ChatHandler = (*ChatHandlerIRC)(nil)

func NewChatHandlerIRC(
	ctx context.Context,
	channelID string,
) (_ret *ChatHandlerIRC, _err error) {
	logger.Debugf(ctx, "NewChatHandlerIRC")
	defer func() { logger.Debugf(ctx, "/NewChatHandlerIRC: %v", _err) }()
	var errs []error
	for attempt := 0; attempt < 3; attempt++ {
		h, err := newChatHandlerIRC(ctx, newChatClientIRC(), channelID)
		if err == nil {
			return h, nil
		}
		err = fmt.Errorf("attempt #%d failed: %w", attempt, err)
		logger.Errorf(ctx, "%v", err)
		errs = append(errs, err)
		time.Sleep(time.Second)
	}
	return nil, errors.Join(errs...)
}

func newChatHandlerIRC(
	ctx context.Context,
	chatClient ChatClientIRC,
	channelID string,
) (_ret *ChatHandlerIRC, _err error) {
	logger.Debugf(ctx, "newChatHandlerIRC")
	defer func() { logger.Debugf(ctx, "/newChatHandlerIRC: %v", _err) }()
	err := chatClient.Join(channelID)
	if err != nil {
		return nil, fmt.Errorf("unable to join channel '%s': %w", channelID, err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	h := &ChatHandlerIRC{
		client:          chatClient,
		cancelFunc:      cancelFn,
		messagesInChan:  make(chan irc.ChatMessage),
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}

	h.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer logger.Debugf(ctx, "newChatHandlerIRC: closed")
		defer h.waitGroup.Done()
		defer func() {
			h.client.Close(ctx)
			// h.Client.Close above waits inside for everything to finish,
			// so we can safely close the channel here:
			close(h.messagesInChan)
			close(h.messagesOutChan)
		}()
		for {
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "newChatHandlerIRC: closing: %v", ctx.Err())
				return
			case ev, ok := <-h.messagesInChan:
				if !ok {
					logger.Debugf(ctx, "newChatHandlerIRC: input channel closed")
					return
				}
				select {
				case h.messagesOutChan <- streamcontrol.ChatMessage{
					CreatedAt: ev.CreatedAt,
					UserID:    streamcontrol.ChatUserID(ev.Sender.Username),
					Username:  ev.Sender.Username,
					MessageID: streamcontrol.ChatMessageID(ev.ID),
					Message:   ev.Text, // TODO: investigate if we need ev.IRCMessage.Text
				}:
				default:
					logger.Warnf(ctx, "the queue is full, skipping the message")
				}
			}
		}
	})

	chatClient.OnShardMessage(h.onShardMessage)
	return h, nil
}

func (h *ChatHandlerIRC) onShardMessage(shard int, msg irc.ChatMessage) {
	ctx := context.TODO()
	logger.Debugf(ctx, "newChatHandlerIRC: onShardMessage")
	h.messagesInChan <- msg
}

func (h *ChatHandlerIRC) Close(ctx context.Context) error {
	h.cancelFunc()
	h.waitGroup.Wait()
	return nil
}

func (h *ChatHandlerIRC) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
