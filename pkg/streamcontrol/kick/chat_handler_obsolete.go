package kick

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/chathandlerobsolete"
)

type ChatHandlerOBSOLETE = chathandlerobsolete.ChatHandlerOBSOLETE

func NewChatHandlerOBSOLETE(
	ctx context.Context,
	channelSlug string,
) (*ChatHandlerOBSOLETE, error) {
	return chathandlerobsolete.New(ctx, channelSlug)
}
