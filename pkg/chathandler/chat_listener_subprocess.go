package chathandler

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

const (
	FlagChatListenerMode        = "internal-chat-listener"
	FlagChatListenerPlatform    = "internal-chat-listener-platform"
	FlagChatListenerType        = "internal-chat-listener-type"
	FlagChatListenerStreamdAddr = "internal-chat-listener-streamd-addr"
)

// RunAsChatListenerIfRequested scans os.Args for the --internal-chat-listener
// flag. If present, it runs as a standalone chat listener subprocess and
// returns true. If absent, it returns false so the caller can proceed with
// normal startup.
//
// os.Args are scanned directly (not via pflag) to avoid conflicts with the
// main binary's flag parsing.
func RunAsChatListenerIfRequested(ctx context.Context) bool {
	if !hasFlag(FlagChatListenerMode) {
		return false
	}

	err := runChatListener(ctx)
	if err != nil {
		logger.Errorf(ctx, "chat listener subprocess failed: %v", err)
		os.Exit(1)
	}

	return true
}

func runChatListener(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "runChatListener")
	defer func() { logger.Tracef(ctx, "/runChatListener: %v", _err) }()

	platform := streamcontrol.PlatformName(flagValue(FlagChatListenerPlatform))
	if platform == "" {
		return fmt.Errorf("--%s is required", FlagChatListenerPlatform)
	}

	listenerTypeName := flagValue(FlagChatListenerType)
	if listenerTypeName == "" {
		return fmt.Errorf("--%s is required", FlagChatListenerType)
	}
	listenerType, err := streamcontrol.ChatListenerTypeFromString(listenerTypeName)
	if err != nil {
		return fmt.Errorf("invalid listener type %q: %w", listenerTypeName, err)
	}

	streamdAddr := flagValue(FlagChatListenerStreamdAddr)
	if streamdAddr == "" {
		return fmt.Errorf("--%s is required", FlagChatListenerStreamdAddr)
	}

	creds := DetectTransportCredentials(ctx, streamdAddr)
	conn, client, err := ConnectToStreamd(ctx, streamdAddr, creds)
	if err != nil {
		return fmt.Errorf("connect to streamd: %w", err)
	}
	defer conn.Close()

	platCfg, err := FetchPlatformConfig(ctx, client, platform)
	if err != nil {
		return fmt.Errorf("fetch platform config: %w", err)
	}

	factory := GetChatListenerFactory(platform)
	if factory == nil {
		return fmt.Errorf("no factory registered for platform %s", platform)
	}

	listener, err := factory.CreateChatListener(ctx, platCfg, listenerType)
	if err != nil {
		return fmt.Errorf("create chat listener: %w", err)
	}

	runner := NewSingleListenerRunner(platform, listenerType, client, listener, RunnerConfig{})
	return runner.Run(ctx)
}

// hasFlag checks whether --<name> is present in os.Args.
func hasFlag(name string) bool {
	target := "--" + name
	for _, arg := range os.Args[1:] {
		if arg == target || strings.HasPrefix(arg, target+"=") {
			return true
		}
	}
	return false
}

// flagValue returns the value for --<name>=<value> or --<name> <value>
// from os.Args. Returns "" if not found.
func flagValue(name string) string {
	target := "--" + name
	for i, arg := range os.Args[1:] {
		switch {
		case strings.HasPrefix(arg, target+"="):
			return arg[len(target)+1:]
		case arg == target && i+1 < len(os.Args[1:]):
			return os.Args[i+2] // +2 because os.Args[1:] starts at index 1
		}
	}
	return ""
}
