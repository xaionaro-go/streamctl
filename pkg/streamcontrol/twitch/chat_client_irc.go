package twitch

import (
	"context"

	"github.com/adeithe/go-twitch"
	"github.com/adeithe/go-twitch/irc"
)

type chatClientIRC struct {
	*irc.Client
}

var _ ChatClientIRC = (*chatClientIRC)(nil)

func (c *chatClientIRC) Join(channelIDs ...string) error {
	return c.Client.Join(channelIDs...)
}
func (c *chatClientIRC) OnShardMessage(callback func(shard int, msg irc.ChatMessage)) {
	c.Client.OnShardMessage(callback)
}
func (c *chatClientIRC) Close(ctx context.Context) error {
	c.Client.Close()
	return nil
}

func newChatClientIRC() *chatClientIRC {
	return &chatClientIRC{
		Client: twitch.IRC(),
	}
}
