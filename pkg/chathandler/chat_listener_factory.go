package chathandler

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// ChatListenerFactory creates platform-specific ChatListener instances.
type ChatListenerFactory interface {
	PlatformName() streamcontrol.PlatformName
	SupportedChatListenerTypes() []streamcontrol.ChatListenerType
	CreateChatListener(
		ctx context.Context,
		platCfg *streamcontrol.AbstractPlatformConfig,
		listenerType streamcontrol.ChatListenerType,
	) (ChatListener, error)
}
