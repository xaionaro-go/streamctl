package twitch

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	twitchtypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

func init() {
	chathandler.RegisterChatListenerFactory(&Factory{})
}

// Factory creates Twitch ChatListener instances.
//
// PACE mapping:
//   - Primary: EventSub (requires ClientID + UserAccessToken)
//   - Contingency: IRC (requires Channel name only)
type Factory struct{}

func (Factory) PlatformName() streamcontrol.PlatformName {
	return twitchtypes.ID
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
	logger.Tracef(ctx, "CreateChatListener[twitch](%s)", listenerType)
	defer func() { logger.Tracef(ctx, "/CreateChatListener[twitch](%s): %v", listenerType, _err) }()

	cfg := streamcontrol.ConvertPlatformConfig[twitch.PlatformSpecificConfig, twitch.StreamProfile](ctx, platCfg)

	switch listenerType {
	case streamcontrol.ChatListenerPrimary:
		return createEventSubListener(ctx, cfg)
	case streamcontrol.ChatListenerContingency:
		return createIRCListener(ctx, cfg)
	default:
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: twitchtypes.ID,
			ListenerType: listenerType,
		}
	}
}

func createEventSubListener(
	ctx context.Context,
	cfg *twitch.Config,
) (_ chathandler.ChatListener, _err error) {
	logger.Tracef(ctx, "createEventSubListener")
	defer func() { logger.Tracef(ctx, "/createEventSubListener: %v", _err) }()

	clientID := cfg.Config.ClientID
	userAccessToken := cfg.Config.UserAccessToken.Get()
	channel := cfg.Config.Channel

	clientSecret := cfg.Config.ClientSecret.Get()

	if clientID == "" || clientSecret == "" || userAccessToken == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: twitchtypes.ID,
			ListenerType: streamcontrol.ChatListenerPrimary,
		}
	}

	if channel == "" {
		return nil, fmt.Errorf("twitch channel is required for EventSub")
	}

	return &EventSubListener{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		AccessToken:  userAccessToken,
		RefreshToken: cfg.Config.RefreshToken.Get(),
		ChannelID:    channel,
	}, nil
}

func createIRCListener(
	ctx context.Context,
	cfg *twitch.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createIRCListener")
	defer func() { logger.Tracef(ctx, "/createIRCListener") }()
	channel := cfg.Config.Channel
	if channel == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: twitchtypes.ID,
			ListenerType: streamcontrol.ChatListenerContingency,
		}
	}

	return &IRCListener{
		ChannelID: channel,
	}, nil
}
