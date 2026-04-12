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
//   - Primary: gRPC stream via youtubeapiproxy (requires yt_proxy_addr)
//   - Alternate: REST API polling (requires api_key; auto-discovers broadcasts via yt_proxy_addr)
//   - Contingency: Obsolete JSON parser (auto-discovers broadcasts via yt_proxy_addr)
//
// All listeners auto-discover active broadcasts when yt_proxy_addr is configured.
// Manual live_chat_id / video_id in Custom config are used as fallbacks.
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

	// ChannelID from typed config; used for search-based detection methods.
	channelID := cfg.Config.ChannelID

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &GRPCStreamListener{
		YTProxyAddr:  ytProxyAddr,
		ChannelID:    channelID,
		DetectMethod: detectMethod,
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

	ytProxyAddr, _ := cfg.GetCustomString("yt_proxy_addr")
	liveChatID, _ := cfg.GetCustomString("live_chat_id")
	videoID, _ := cfg.GetCustomString("video_id")

	// Need either a pre-set liveChatID or yt_proxy_addr for auto-discovery.
	if liveChatID == "" && ytProxyAddr == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerAlternate,
		}
	}

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &PollingListener{
		APIKey:       apiKey,
		YTProxyAddr:  ytProxyAddr,
		ChannelID:    cfg.Config.ChannelID,
		DetectMethod: detectMethod,
		LiveChatID:   liveChatID,
		VideoID:      videoID,
	}, nil
}

func createObsoleteListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createObsoleteListener")
	defer func() { logger.Tracef(ctx, "/createObsoleteListener") }()

	ytProxyAddr, _ := cfg.GetCustomString("yt_proxy_addr")
	videoID, _ := cfg.GetCustomString("video_id")

	// Need either a pre-set videoID or yt_proxy_addr for auto-discovery.
	if videoID == "" && ytProxyAddr == "" {
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: streamcontrol.ChatListenerContingency,
		}
	}

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &ObsoleteJSONListener{
		YTProxyAddr:  ytProxyAddr,
		ChannelID:    cfg.Config.ChannelID,
		DetectMethod: detectMethod,
		VideoID:      videoID,
	}, nil
}
