package twitch

import (
	"context"
	"fmt"
	"sync"

	"github.com/adeithe/go-twitch/irc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
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
	closeOnce       sync.Once
	messagesInChan  chan irc.ChatMessage
	messagesOutChan chan streamcontrol.ChatEvent
}

func NewChatHandler(
	channelID string,
) (*ChatHandler, error) {
	return newChatHandler(newChatClient(), channelID)
}

func newChatHandler(
	chatClient ChatClient,
	channelID string,
) (*ChatHandler, error) {
	err := chatClient.Join(channelID)
	if err != nil {
		return nil, fmt.Errorf("unable to join channel '%s': %w", channelID, err)
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	h := &ChatHandler{
		client:          chatClient,
		cancelFunc:      cancelFn,
		messagesInChan:  make(chan irc.ChatMessage),
		messagesOutChan: make(chan streamcontrol.ChatEvent),
	}

	observability.Go(ctx, func() {
		defer func() {
			close(h.messagesOutChan)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-h.messagesInChan:
				h.messagesOutChan <- streamcontrol.ChatEvent{
					UserID:    ev.Sender.Username,
					MessageID: ev.ID,
					Message:   ev.Text, // TODO: investigate if we need ev.IRCMessage.Text
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
	h.closeOnce.Do(func() {
		h.cancelFunc()
		h.client.Close()
		// h.Client.Close above waits inside for everything to finish,
		// so we can safely close the channel here:
		close(h.messagesInChan)
	})
	return nil
}

func (h *ChatHandler) MessagesChan() <-chan streamcontrol.ChatEvent {
	return h.messagesOutChan
}
