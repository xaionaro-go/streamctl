package twitch

import (
	"github.com/adeithe/go-twitch"
	"github.com/adeithe/go-twitch/irc"
)

type chatClientImpl struct {
	*irc.Client
}

var _ ChatClient = (*chatClientImpl)(nil)

func (c *chatClientImpl) Join(channelIDs ...string) error {
	return c.Client.Join(channelIDs...)
}
func (c *chatClientImpl) OnShardMessage(callback func(shard int, msg irc.ChatMessage)) {
	c.Client.OnShardMessage(callback)
}

func newChatClient() *chatClientImpl {
	return &chatClientImpl{
		Client: twitch.IRC(),
	}
}
