package kick

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete"
)

type ChatHandlerOBSOLETE = chathandlerobsolete.ChatHandlerOBSOLETE

func (k *Kick) newChatHandlerOBSOLETE(
	ctx context.Context,
	channelSlug string,
) (*ChatHandlerOBSOLETE, error) {
	return NewChatHandlerOBSOLETE(ctx, channelSlug)
}

func NewChatHandlerOBSOLETE(
	ctx context.Context,
	channelSlug string,
) (*ChatHandlerOBSOLETE, error) {
	return chathandlerobsolete.New(ctx, channelSlug)
}
