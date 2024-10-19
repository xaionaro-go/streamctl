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

type chatClientMock struct {
	join           func(channelIDs ...string) error
	onShardMessage func(func(shard int, msg irc.ChatMessage))
	close          func()
}

var _ ChatClient = (*chatClientMock)(nil)

func (c *chatClientMock) Join(channelIDs ...string) error {
	return c.join(channelIDs...)
}
func (c *chatClientMock) OnShardMessage(callback func(shard int, msg irc.ChatMessage)) {
	c.onShardMessage(callback)
}
func (c *chatClientMock) Close() {
	c.close()
}

func TestChatHandler(t *testing.T) {
	ctx := context.TODO()
	const channelID = "test-channel-id"

	var (
		joinedChannelIDs []string
		callback         func(shard int, msg irc.ChatMessage)
		closeCount       = 0
	)
	h, err := newChatHandler(ctx, &chatClientMock{
		join: func(channelIDs ...string) error {
			joinedChannelIDs = append(joinedChannelIDs, channelIDs...)
			return nil
		},
		onShardMessage: func(_callback func(shard int, msg irc.ChatMessage)) {
			callback = _callback
		},
		close: func() {
			closeCount++
		},
	}, channelID)
	require.NoError(t, err)

	expectedEvent := streamcontrol.ChatMessage{
		UserID:    "user-id",
		MessageID: "message-id",
		Message:   "some\nmulti line\n message",
	}

	messagesCount := 0
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range h.messagesOutChan {
			require.Equal(t, expectedEvent, ev)
			messagesCount++
		}
	}()

	require.Equal(t, []string{channelID}, joinedChannelIDs)

	callback(0, irc.ChatMessage{
		Sender: irc.ChatSender{
			Username: expectedEvent.UserID,
		},
		ID:        expectedEvent.MessageID,
		Channel:   channelID,
		Text:      expectedEvent.Message,
		CreatedAt: time.Now(),
	})

	require.Equal(t, 0, closeCount)
	h.Close()
	require.Equal(t, 1, closeCount)
	wg.Wait()
}
