package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/nicklaw5/helix/v2"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func main() {
	streamdAddr := pflag.String("streamd-addr", "localhost:3594", "streamd gRPC address")
	channelID := pflag.String("channel-id", "", "Twitch channel ID or login name (required)")
	clientID := pflag.String("client-id", "", "Twitch app client ID (enables EventSub; IRC-only if omitted)")
	clientSecret := pflag.String("client-secret", "", "Twitch app client secret (enables EventSub)")
	// Pre-authorized tokens from streamd's token store. Required for EventSub
	// in daemon mode — the handler runs headless and cannot do interactive OAuth.
	accessToken := pflag.String("access-token", "", "pre-authorized Twitch user access token (for EventSub)")
	refreshToken := pflag.String("refresh-token", "", "pre-authorized Twitch refresh token (for EventSub)")
	var logLevel logger.Level
	pflag.Var(&logLevel, "log-level", "log level")
	pflag.Parse()

	if logLevel == logger.LevelUndefined {
		logLevel = logger.LevelDebug
	}
	observability.LogLevelFilter.SetLevel(logLevel)

	l := logrus.Default()
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx = logger.CtxWithLogger(ctx, l.WithLevel(logLevel))
	defer belt.Flush(ctx)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if *channelID == "" {
		fmt.Fprintf(os.Stderr, "fatal: --channel-id is required\n")
		os.Exit(1)
	}

	cfg := twitchHandlerConfig{
		streamdAddr:  *streamdAddr,
		channelID:    *channelID,
		clientID:     *clientID,
		clientSecret: *clientSecret,
		accessToken:  *accessToken,
		refreshToken: *refreshToken,
	}

	if err := run(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

type twitchHandlerConfig struct {
	streamdAddr  string
	channelID    string
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
}

func run(
	ctx context.Context,
	cfg twitchHandlerConfig,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	creds := chathandler.DetectTransportCredentials(ctx, cfg.streamdAddr)
	conn, streamdClient, err := chathandler.ConnectToStreamd(ctx, cfg.streamdAddr, creds)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Disable built-in chat listener for Twitch since we're taking over.
	_, err = streamdClient.SetBuiltinChatListenerEnabled(ctx, &streamd_grpc.SetBuiltinChatListenerEnabledRequest{
		PlatID:  string(twitch.ID),
		Enabled: false,
	})
	if err != nil {
		logger.Warnf(ctx, "failed to disable built-in Twitch chat listener: %v", err)
	}

	var primary chathandler.ChatListener
	var fallback chathandler.ChatListener

	hasEventSubCreds := cfg.clientID != "" && cfg.clientSecret != "" && cfg.accessToken != ""
	switch {
	case hasEventSubCreds:
		primary = &eventSubListener{
			clientID:     cfg.clientID,
			clientSecret: cfg.clientSecret,
			accessToken:  cfg.accessToken,
			refreshToken: cfg.refreshToken,
			channelID:    cfg.channelID,
		}
		fallback = &ircListener{channelID: cfg.channelID}
	default:
		// No EventSub credentials — use IRC as primary, no Level 1 fallback.
		logger.Warnf(ctx, "no client-id/client-secret/access-token provided; using IRC-only mode (no EventSub)")
		primary = &ircListener{channelID: cfg.channelID}
		fallback = &ircListener{channelID: cfg.channelID}
	}

	runner := chathandler.NewRunner(
		twitch.ID,
		streamdClient,
		primary,
		fallback,
		chathandler.RunnerConfig{},
	)

	return runner.Run(ctx)
}

// eventSubListener implements chathandler.ChatListener using Twitch EventSub WebSocket.
// Uses pre-authorized tokens from streamd — no interactive OAuth required.
type eventSubListener struct {
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
	channelID    string
	handler      *twitch.ChatHandlerSub
}

func (l *eventSubListener) Name() string { return "EventSub" }

func (l *eventSubListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	client, err := helix.NewClientWithContext(ctx, &helix.Options{
		ClientID:     l.clientID,
		ClientSecret: l.clientSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("create helix client: %w", err)
	}

	client.SetUserAccessToken(l.accessToken)
	if l.refreshToken != "" {
		client.SetRefreshToken(l.refreshToken)
	}

	userID, err := twitch.GetUserID(ctx, client, l.channelID)
	if err != nil {
		return nil, fmt.Errorf("get user ID for %q: %w", l.channelID, err)
	}

	h, err := twitch.NewChatHandlerSub(ctx, client, userID, nil)
	if err != nil {
		return nil, fmt.Errorf("create EventSub handler: %w", err)
	}
	l.handler = h

	return h.MessagesChan(), nil
}

func (l *eventSubListener) Close(ctx context.Context) error {
	if l.handler != nil {
		return l.handler.Close(ctx)
	}
	return nil
}

// ircListener implements chathandler.ChatListener using Twitch IRC (anonymous, no auth needed).
type ircListener struct {
	channelID string
	handler   *twitch.ChatHandlerIRC
}

func (l *ircListener) Name() string { return "IRC" }

func (l *ircListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	h, err := twitch.NewChatHandlerIRC(ctx, l.channelID)
	if err != nil {
		return nil, fmt.Errorf("create IRC handler: %w", err)
	}
	l.handler = h

	return h.MessagesChan(), nil
}

func (l *ircListener) Close(ctx context.Context) error {
	if l.handler != nil {
		return l.handler.Close(ctx)
	}
	return nil
}
