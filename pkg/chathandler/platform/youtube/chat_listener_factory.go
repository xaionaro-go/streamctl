package youtube

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	ytpkg "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	yttypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"google.golang.org/api/option"
	youtubesvc "google.golang.org/api/youtube/v3"
)

func init() {
	chathandler.RegisterChatListenerFactory(&Factory{})
}

// Factory creates YouTube ChatListener instances.
//
// PACE mapping:
//   - Primary: gRPC stream via youtubeapiproxy (requires YTProxyAddr),
//     falls back to OAuth2-based polling when proxy is unavailable.
//   - Alternate: REST API polling (prefers APIKey, falls back to OAuth2).
//   - Contingency: Obsolete JSON parser (auto-discovers via YTProxyAddr).
//
// All listeners auto-discover active broadcasts when possible.
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
		return createPrimaryListener(ctx, cfg)
	case streamcontrol.ChatListenerAlternate:
		return createAlternateListener(ctx, cfg)
	case streamcontrol.ChatListenerContingency:
		return createObsoleteListener(ctx, cfg)
	default:
		return nil, chathandler.ErrChatListenerTypeNotImplemented{
			PlatformName: yttypes.ID,
			ListenerType: listenerType,
		}
	}
}

// createPrimaryListener tries gRPC stream first, then falls back to OAuth2 polling.
func createPrimaryListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createPrimaryListener")
	defer func() { logger.Tracef(ctx, "/createPrimaryListener") }()

	ytProxyAddr := cfg.Config.YTProxyAddr

	// Prefer gRPC stream via youtubeapiproxy when available.
	if ytProxyAddr != "" {
		return createGRPCStreamListener(ctx, cfg)
	}

	// Fall back to OAuth2-based polling when proxy is unavailable.
	if hasOAuth2Credentials(cfg) {
		logger.Debugf(ctx, "no yt_proxy_addr; using OAuth2-based polling as primary listener")
		return createOAuth2PollingListener(ctx, cfg)
	}

	return nil, chathandler.ErrChatListenerMisconfigured{
		PlatformName:  yttypes.ID,
		ListenerType:  streamcontrol.ChatListenerPrimary,
		MissingFields: []string{"yt_proxy_addr or OAuth2 credentials (ClientID, ClientSecret, Token)"},
	}
}

// createAlternateListener tries API-key polling first, then falls back to OAuth2 polling.
func createAlternateListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createAlternateListener")
	defer func() { logger.Tracef(ctx, "/createAlternateListener") }()

	apiKey := cfg.Config.APIKey

	// Prefer API-key polling when available.
	if apiKey != "" {
		return createAPIKeyPollingListener(ctx, cfg)
	}

	// Fall back to OAuth2-based polling (only when Primary used gRPC,
	// otherwise this would duplicate Primary).
	if cfg.Config.YTProxyAddr != "" && hasOAuth2Credentials(cfg) {
		logger.Debugf(ctx, "no api_key; using OAuth2-based polling as alternate listener")
		return createOAuth2PollingListener(ctx, cfg)
	}

	return nil, chathandler.ErrChatListenerMisconfigured{
		PlatformName:  yttypes.ID,
		ListenerType:  streamcontrol.ChatListenerAlternate,
		MissingFields: []string{"api_key or OAuth2 credentials (ClientID, ClientSecret, Token)"},
	}
}

func createGRPCStreamListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createGRPCStreamListener")
	defer func() { logger.Tracef(ctx, "/createGRPCStreamListener") }()

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &GRPCStreamListener{
		YTProxyAddr:  cfg.Config.YTProxyAddr,
		ChannelID:    cfg.Config.ChannelID,
		DetectMethod: detectMethod,
	}, nil
}

func createAPIKeyPollingListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createAPIKeyPollingListener")
	defer func() { logger.Tracef(ctx, "/createAPIKeyPollingListener") }()

	svc, err := youtubesvc.NewService(ctx, option.WithAPIKey(cfg.Config.APIKey))
	if err != nil {
		return nil, fmt.Errorf("create YouTube service with API key: %w", err)
	}

	liveChatID, _ := cfg.GetCustomString("live_chat_id")
	videoID, _ := cfg.GetCustomString("video_id")

	// API-key polling cannot discover broadcasts on its own; need either
	// a pre-set liveChatID or yt_proxy_addr for auto-discovery.
	if liveChatID == "" && cfg.Config.YTProxyAddr == "" {
		return nil, chathandler.ErrChatListenerMisconfigured{
			PlatformName:  yttypes.ID,
			ListenerType:  streamcontrol.ChatListenerAlternate,
			MissingFields: []string{"live_chat_id or yt_proxy_addr (needed for broadcast discovery with API key)"},
		}
	}

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &PollingListener{
		Service:      svc,
		YTProxyAddr:  cfg.Config.YTProxyAddr,
		ChannelID:    cfg.Config.ChannelID,
		DetectMethod: detectMethod,
		LiveChatID:   liveChatID,
		VideoID:      videoID,
	}, nil
}

func createOAuth2PollingListener(
	ctx context.Context,
	cfg *ytpkg.Config,
) (chathandler.ChatListener, error) {
	logger.Tracef(ctx, "createOAuth2PollingListener")
	defer func() { logger.Tracef(ctx, "/createOAuth2PollingListener") }()

	svc, err := newOAuth2YouTubeService(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create OAuth2 YouTube service: %w", err)
	}

	liveChatID, _ := cfg.GetCustomString("live_chat_id")
	videoID, _ := cfg.GetCustomString("video_id")

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	// OAuth2 polling can discover broadcasts via liveBroadcasts.list mine=true
	// without a proxy, so no proxy requirement here.
	return &PollingListener{
		Service:      svc,
		YTProxyAddr:  cfg.Config.YTProxyAddr,
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

	videoID, _ := cfg.GetCustomString("video_id")

	// Need either a pre-set videoID or YTProxyAddr for auto-discovery.
	if videoID == "" && cfg.Config.YTProxyAddr == "" {
		return nil, chathandler.ErrChatListenerMisconfigured{
			PlatformName:  yttypes.ID,
			ListenerType:  streamcontrol.ChatListenerContingency,
			MissingFields: []string{"video_id or yt_proxy_addr"},
		}
	}

	detectMethod := DetectMethodBroadcasts
	if dm, ok := cfg.GetCustomString("detect_method"); ok && dm != "" {
		detectMethod = DetectMethod(dm)
	}

	return &ObsoleteJSONListener{
		YTProxyAddr:  cfg.Config.YTProxyAddr,
		ChannelID:    cfg.Config.ChannelID,
		DetectMethod: detectMethod,
		VideoID:      videoID,
	}, nil
}
