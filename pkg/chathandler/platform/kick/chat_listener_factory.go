package kick

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	chatwebhookclient "github.com/xaionaro-go/chatwebhook/pkg/grpc/client"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kickpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	kicktypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
)

func init() {
	chathandler.RegisterChatListenerFactory(&Factory{})
}

// Factory creates Kick ChatListener instances.
//
// PACE mapping:
//   - Primary: WebSocket via chatwebhook
//   - Contingency: Obsolete HTTP handler (requires Channel slug)
type Factory struct{}

func (Factory) PlatformName() streamcontrol.PlatformName {
	return kicktypes.ID
}

func (Factory) SupportedChatListenerTypes() []streamcontrol.ChatListenerType {
	return []streamcontrol.ChatListenerType{
		streamcontrol.ChatListenerPrimary,
		streamcontrol.ChatListenerContingency,
	}
}

func (Factory) CreateChatListener(
	ctx context.Context,
	platCfg *streamcontrol.AbstractPlatformConfig,
	listenerType streamcontrol.ChatListenerType,
) (_ chathandler.ChatListener, _err error) {
	logger.Tracef(ctx, "CreateChatListener[kick](%s)", listenerType)
	defer func() { logger.Tracef(ctx, "/CreateChatListener[kick](%s): %v", listenerType, _err) }()

	cfg := streamcontrol.ConvertPlatformConfig[kickpkg.PlatformSpecificConfig, kickpkg.StreamProfile](ctx, platCfg)

	switch listenerType {
	case streamcontrol.ChatListenerPrimary:
		return createWebSocketListener(ctx, cfg)
	case streamcontrol.ChatListenerContingency:
		return createObsoleteListener(ctx, cfg)
	default:
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: kicktypes.ID,
			ListenerType: listenerType,
		}
	}
}

func createWebSocketListener(
	ctx context.Context,
	cfg *kickpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createWebSocketListener")
	defer func() { logger.Tracef(ctx, "/createWebSocketListener") }()
	addr, ok := cfg.GetCustomString("chatwebhook_addr")
	if !ok || addr == "" {
		addr = chatwebhookclient.DefaultServerAddress
	}

	return &WebSocketListener{
		ChatWebhookAddr: addr,
	}, nil
}

func createObsoleteListener(
	ctx context.Context,
	cfg *kickpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createObsoleteListener")
	defer func() { logger.Tracef(ctx, "/createObsoleteListener") }()
	channel := cfg.Config.Channel
	if channel == "" {
		return nil, chathandler.ErrChatListenerMisconfigured{
			PlatformName:  kicktypes.ID,
			ListenerType:  streamcontrol.ChatListenerContingency,
			MissingFields: []string{"Channel"},
		}
	}

	return &ObsoleteListener{
		ChannelSlug: channel,
	}, nil
}
