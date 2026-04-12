package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	twitchtypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

// twitchSourceNameToListenerType maps chatinjector source names to
// ChatListenerType values used by the shared factory.
func twitchSourceNameToListenerType(name string) (streamcontrol.ChatListenerType, error) {
	switch name {
	case "eventsub":
		return streamcontrol.ChatListenerPrimary, nil
	case "irc":
		return streamcontrol.ChatListenerContingency, nil
	default:
		return 0, fmt.Errorf("unknown twitch source type %q", name)
	}
}

// newTwitchSourceFromFactory creates a ChatSource for Twitch using the shared
// ChatListenerFactory. It does not perform internal fallback — callers
// (newTwitchSources) handle fallback via FallbackSource.
func newTwitchSourceFromFactory(
	ctx context.Context,
	pc PlatformConfig,
	listenerType streamcontrol.ChatListenerType,
) (ChatSource, error) {
	logger.Tracef(ctx, "newTwitchSourceFromFactory(%s)", listenerType)
	defer func() { logger.Tracef(ctx, "/newTwitchSourceFromFactory(%s)", listenerType) }()

	factory := chathandler.GetChatListenerFactory(twitchtypes.ID)
	if factory == nil {
		return nil, fmt.Errorf("no ChatListenerFactory registered for %s", twitchtypes.ID)
	}

	platCfg, err := toAbstractPlatformConfig(pc)
	if err != nil {
		return nil, err
	}

	listener, err := factory.CreateChatListener(ctx, platCfg, listenerType)
	if err != nil {
		return nil, fmt.Errorf("create twitch listener (%s): %w", listenerType, err)
	}

	return &ChatSourceFromListener{
		Listener:     listener,
		PlatformName: twitchtypes.ID,
	}, nil
}
