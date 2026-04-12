package youtube

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	yttypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
)

func init() {
	chathandler.RegisterChatListenerFactory(&Factory{})
}

// Factory creates YouTube ChatListener instances.
//
// PACE mapping:
//   - Primary: gRPC stream via youtubeapiproxy (requires yt_proxy_addr in Custom config)
//   - Alternate: REST API polling (requires API key in Custom config)
//   - Contingency: Obsolete JSON parser via ChatListenerOBSOLETE (requires video_id)
type Factory struct{}

func (Factory) PlatformName() streamcontrol.PlatformName {
	return yttypes.ID
}

func (Factory) SupportedChatListenerTypes() []streamcontrol.ChatListenerType {
	return []streamcontrol.ChatListenerType{
		streamcontrol.ChatListenerPrimary,
		streamcontrol.ChatListenerAlternate,
		streamcontrol.ChatListenerContingency,
	}
}

func (Factory) CreateChatListener(
	ctx context.Context,
	platCfg *streamcontrol.AbstractPlatformConfig,
	listenerType streamcontrol.ChatListenerType,
) (_ chathandler.ChatListener, _err error) {
	logger.Tracef(ctx, "CreateChatListener[youtube](%s)", listenerType)
	defer func() { logger.Tracef(ctx, "/CreateChatListener[youtube](%s): %v", listenerType, _err) }()

	cfg := streamcontrol.ConvertPlatformConfig[ytpkg.PlatformSpecificConfig, ytpkg.StreamProfile](ctx, platCfg)

	switch listenerType {
	case streamcontrol.ChatListenerPrimary:
		return createGRPCStreamListener(ctx, cfg)
	case streamcontrol.ChatListenerAlternate:
		return createPollingListener(ctx, cfg)
	case streamcontrol.ChatListenerContingency:
		return createObsoleteListener(ctx, cfg)
	default:
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: listenerType,
		}
	}
}

func createGRPCStreamListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createGRPCStreamListener")
	defer func() { logger.Tracef(ctx, "/createGRPCStreamListener") }()
	ytProxyAddr, ok := cfg.GetCustomString("yt_proxy_addr")
	if !ok || ytProxyAddr == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerPrimary,
		}
	}

	liveChatID, ok := cfg.GetCustomString("live_chat_id")
	if !ok || liveChatID == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerPrimary,
		}
	}

	return &GRPCStreamListener{
		YTProxyAddr: ytProxyAddr,
		LiveChatID:  liveChatID,
	}, nil
}

func createPollingListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createPollingListener")
	defer func() { logger.Tracef(ctx, "/createPollingListener") }()
	apiKey, ok := cfg.GetCustomString("api_key")
	if !ok || apiKey == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerAlternate,
		}
	}

	liveChatID, ok := cfg.GetCustomString("live_chat_id")
	if !ok || liveChatID == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerAlternate,
		}
	}

	videoID, _ := cfg.GetCustomString("video_id")

	return &PollingListener{
		APIKey:     apiKey,
		LiveChatID: liveChatID,
		VideoID:    videoID,
	}, nil
}

func createObsoleteListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createObsoleteListener")
	defer func() { logger.Tracef(ctx, "/createObsoleteListener") }()
	videoID, ok := cfg.GetCustomString("video_id")
	if !ok || videoID == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerContingency,
		}
	}

	return &ObsoleteJSONListener{
		VideoID: videoID,
	}, nil
}
