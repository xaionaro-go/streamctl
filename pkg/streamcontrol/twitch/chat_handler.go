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

type ChatClient interface {
	Join(channelIDs ...string) error
	OnShardMessage(func(shard int, msg irc.ChatMessage))
	Close()
}

type ChatHandler struct {
	client          ChatClient
	cancelFunc      context.CancelFunc
	waitGroup       sync.WaitGroup
	messagesInChan  chan irc.ChatMessage
	messagesOutChan chan streamcontrol.ChatMessage
}

func NewChatHandler(
	ctx context.Context,
	channelID string,
) (*ChatHandler, error) {
	var errs []error
	for attempt := 0; attempt < 3; attempt++ {
		h, err := newChatHandler(ctx, newChatClient(), channelID)
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

func newChatHandler(
	ctx context.Context,
	chatClient ChatClient,
	channelID string,
) (*ChatHandler, error) {
	err := chatClient.Join(channelID)
	if err != nil {
		return nil, fmt.Errorf("unable to join channel '%s': %w", channelID, err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	h := &ChatHandler{
		client:          chatClient,
		cancelFunc:      cancelFn,
		messagesInChan:  make(chan irc.ChatMessage),
		messagesOutChan: make(chan streamcontrol.ChatMessage, 100),
	}

	h.waitGroup.Add(1)
	observability.Go(ctx, func() {
		defer h.waitGroup.Done()
		defer func() {
			h.client.Close()
			// h.Client.Close above waits inside for everything to finish,
			// so we can safely close the channel here:
			close(h.messagesInChan)
			close(h.messagesOutChan)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-h.messagesInChan:
				select {
				case h.messagesOutChan <- streamcontrol.ChatMessage{
					CreatedAt: ev.CreatedAt,
					UserID:    streamcontrol.ChatUserID(ev.Sender.Username),
					Username:  ev.Sender.Username,
					MessageID: streamcontrol.ChatMessageID(ev.ID),
					Message:   ev.Text, // TODO: investigate if we need ev.IRCMessage.Text
				}:
				default:
				}
			}
		}
	})

	chatClient.OnShardMessage(h.onShardMessage)
	return h, nil
}

func (h *ChatHandler) onShardMessage(shard int, msg irc.ChatMessage) {
	h.messagesInChan <- msg
}

func (h *ChatHandler) Close() error {
	h.cancelFunc()
	h.waitGroup.Wait()
	return nil
}

func (h *ChatHandler) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
