package main

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	kicktypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
)

// newKickSourceFromFactory creates a ChatSource for Kick using the shared
// ChatListenerFactory.
func newKickSourceFromFactory(
	ctx context.Context,
	pc PlatformConfig,
) (ChatSource, error) {
	logger.Tracef(ctx, "newKickSourceFromFactory")
	defer func() { logger.Tracef(ctx, "/newKickSourceFromFactory") }()

	factory := chathandler.GetChatListenerFactory(kicktypes.ID)
	if factory == nil {
		return nil, fmt.Errorf("no ChatListenerFactory registered for %s", kicktypes.ID)
	}

	platCfg, err := toAbstractPlatformConfig(pc)
	if err != nil {
		return nil, err
	}

	listener, err := factory.CreateChatListener(ctx, platCfg, streamcontrol.ChatListenerPrimary)
	if err != nil {
		return nil, fmt.Errorf("create kick listener: %w", err)
	}

	return &ChatSourceFromListener{
		Listener:     listener,
		PlatformName: kicktypes.ID,
	}, nil
}
