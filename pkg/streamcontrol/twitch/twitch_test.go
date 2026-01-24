package twitch

import (
	"context"
	"strconv"
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

	expectedEvent := streamcontrol.Event{
		Type: 1,
		User: streamcontrol.User{
			ID:   "2",
			Slug: "user-slug",
			Name: "user-name",
		},
		ID: "message-id",
		Message: &streamcontrol.Message{
			Content: "some\nmulti line\n message",
			Format:  streamcontrol.TextFormatTypePlain,
		},
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
			ID:          parseInt64(expectedEvent.User.ID),
			DisplayName: expectedEvent.User.Name,
			Username:    string(expectedEvent.User.Slug),
		},
		ID:        string(expectedEvent.ID),
		Channel:   channelID,
		Text:      expectedEvent.Message.Content,
		CreatedAt: time.Now(),
	})

	require.Equal(t, 0, closeCount)
	h.Close(ctx)
	require.Equal(t, 1, closeCount)
	wg.Wait()
}

func parseInt64(s streamcontrol.UserID) int64 {
	v, err := strconv.ParseInt(string(s), 10, 64)
	if err != nil {
		panic(err)
	}
	return v
}
