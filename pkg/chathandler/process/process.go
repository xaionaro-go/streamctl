// Package process registers the chat listener subprocess entrypoint.
// Blank-import this from any binary that spawns external chat handlers.
//
// The init function intercepts --internal-chat-listener flags before
// pflag.Parse() runs, so the subprocess executes before the main
// binary's flag parsing rejects the unknown flags.
package process

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	if !hasFlag(chathandler.FlagChatListenerMode) {
		return
	}

	ctx := context.Background()
	err := run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chat listener subprocess failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	platform := streamcontrol.PlatformName(flagValue(chathandler.FlagChatListenerPlatform))
	if platform == "" {
		return fmt.Errorf("--%s is required", chathandler.FlagChatListenerPlatform)
	}

	listenerTypeName := flagValue(chathandler.FlagChatListenerType)
	if listenerTypeName == "" {
		return fmt.Errorf("--%s is required", chathandler.FlagChatListenerType)
	}
	listenerType, err := streamcontrol.ChatListenerTypeFromString(listenerTypeName)
	if err != nil {
		return fmt.Errorf("invalid listener type %q: %w", listenerTypeName, err)
	}

	streamdAddr := flagValue(chathandler.FlagChatListenerStreamdAddr)
	if streamdAddr == "" {
		return fmt.Errorf("--%s is required", chathandler.FlagChatListenerStreamdAddr)
	}

	creds := chathandler.DetectTransportCredentials(ctx, streamdAddr)
	conn, client, err := chathandler.ConnectToStreamd(ctx, streamdAddr, creds)
	if err != nil {
		return fmt.Errorf("connect to streamd: %w", err)
	}
	defer conn.Close()

	platCfg, err := chathandler.FetchPlatformConfig(ctx, client, platform)
	if err != nil {
		return fmt.Errorf("fetch platform config: %w", err)
	}

	factory := chathandler.GetChatListenerFactory(platform)
	if factory == nil {
		return fmt.Errorf("no factory registered for platform %s", platform)
	}

	listener, err := factory.CreateChatListener(ctx, platCfg, listenerType)
	if err != nil {
		return fmt.Errorf("create chat listener: %w", err)
	}

	runner := chathandler.NewSingleListenerRunner(platform, listenerType, client, listener, chathandler.RunnerConfig{})
	return runner.Run(ctx)
}

func hasFlag(name string) bool {
	target := "--" + name
	for _, arg := range os.Args[1:] {
		if arg == target || strings.HasPrefix(arg, target+"=") {
			return true
		}
	}
	return false
}

func flagValue(name string) string {
	target := "--" + name
	for i, arg := range os.Args[1:] {
		switch {
		case strings.HasPrefix(arg, target+"="):
			return arg[len(target)+1:]
		case arg == target && i+1 < len(os.Args[1:]):
			return os.Args[i+2]
		}
	}
	return ""
}
