package twitch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/adeithe/go-twitch/irc"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type chatClientIRCMock struct {
	join           func(channelIDs ...string) error
	onShardMessage func(func(shard int, msg irc.ChatMessage))
	close          func(ctx context.Context) error
}

var _ ChatClientIRC = (*chatClientIRCMock)(nil)

func (c *chatClientIRCMock) Join(channelIDs ...string) error {
	return c.join(channelIDs...)
}
func (c *chatClientIRCMock) OnShardMessage(callback func(shard int, msg irc.ChatMessage)) {
	c.onShardMessage(callback)
}
func (c *chatClientIRCMock) Close(ctx context.Context) error {
	return c.close(ctx)
}

func TestChatHandlerIRC(t *testing.T) {
	ctx := context.TODO()
	const channelID = "test-channel-id"

	var (
		joinedChannelIDs []string
		callback         func(shard int, msg irc.ChatMessage)
		closeCount       = 0
	)
	h, err := newChatHandlerIRC(ctx, &chatClientIRCMock{
		join: func(channelIDs ...string) error {
			joinedChannelIDs = append(joinedChannelIDs, channelIDs...)
			return nil
		},
		onShardMessage: func(_callback func(shard int, msg irc.ChatMessage)) {
			callback = _callback
		},
		close: func(ctx context.Context) error {
			closeCount++
			return nil
		},
	}, channelID)
	require.NoError(t, err)

	expectedEvent := streamcontrol.ChatMessage{
		EventType: 1,
		UserID:    "user-id",
		Username:  "user-id",
		MessageID: "message-id",
		Message:   "some\nmulti line\n message",
		MessageFormatType: 1,
	}

	messagesCount := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range h.messagesOutChan {
			ev.CreatedAt = time.Time{}
			require.Equal(t, expectedEvent, ev)
			messagesCount++
		}
	}()

	require.Equal(t, []string{channelID}, joinedChannelIDs)

	callback(0, irc.ChatMessage{
		Sender: irc.ChatSender{
			Username: string(expectedEvent.UserID),
		},
		ID:        string(expectedEvent.MessageID),
		Channel:   channelID,
		Text:      expectedEvent.Message,
		CreatedAt: time.Now(),
	})

	require.Equal(t, 0, closeCount)
	h.Close(ctx)
	require.Equal(t, 1, closeCount)
	wg.Wait()
}
