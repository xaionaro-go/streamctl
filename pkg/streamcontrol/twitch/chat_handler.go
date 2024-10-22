package twitch

import (
	"context"
	"fmt"

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
	messagesInChan  chan irc.ChatMessage
	messagesOutChan chan streamcontrol.ChatMessage
}

func NewChatHandler(
	ctx context.Context,
	channelID string,
) (*ChatHandler, error) {
	return newChatHandler(ctx, newChatClient(), channelID)
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

	observability.Go(ctx, func() {
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
	return nil
}

func (h *ChatHandler) MessagesChan() <-chan streamcontrol.ChatMessage {
	return h.messagesOutChan
}
