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
	"github.com/spf13/pflag"
	chatwebhookclient "github.com/xaionaro-go/chatwebhook/pkg/grpc/client"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	kicktypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func main() {
	streamdAddr := pflag.String("streamd-addr", "localhost:3594", "streamd gRPC address")
	chatwebhookAddr := pflag.String("chatwebhook-addr", chatwebhookclient.DefaultServerAddress, "chatwebhook gRPC server address")
	channelSlug := pflag.String("channel-slug", "", "Kick channel slug (for obsolete fallback handler)")
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

	if err := run(ctx, *streamdAddr, *chatwebhookAddr, *channelSlug); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	streamdAddr string,
	chatwebhookAddr string,
	channelSlug string,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	creds := chathandler.DetectTransportCredentials(ctx, streamdAddr)
	conn, streamdClient, err := chathandler.ConnectToStreamd(ctx, streamdAddr, creds)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Disable built-in chat listener for Kick since we're taking over.
	_, err = streamdClient.SetBuiltinChatListenerEnabled(ctx, &streamd_grpc.SetBuiltinChatListenerEnabledRequest{
		PlatID:  string(kicktypes.ID),
		Enabled: false,
	})
	if err != nil {
		logger.Warnf(ctx, "failed to disable built-in Kick chat listener: %v", err)
	}

	primary := &websocketListener{
		chatwebhookAddr: chatwebhookAddr,
	}

	// Fallback: uses the same WebSocket approach (reconnect with fresh client).
	// The obsolete handler requires channel-slug and is used only if provided.
	var fallback chathandler.ChatListener
	switch {
	case channelSlug != "":
		fallback = &obsoleteListener{channelSlug: channelSlug}
	default:
		// No channel-slug → fallback is another WebSocket attempt.
		// Level 2 (streamd process-level) handles total failure.
		fallback = &websocketListener{
			chatwebhookAddr: chatwebhookAddr,
		}
	}

	runner := chathandler.NewRunner(
		kicktypes.ID,
		streamdClient,
		primary,
		fallback,
		chathandler.RunnerConfig{},
	)

	return runner.Run(ctx)
}

// websocketListener implements chathandler.ChatListener using Kick WebSocket via chatwebhook.
type websocketListener struct {
	chatwebhookAddr string
	handler         *kick.ChatHandler
}

func (l *websocketListener) Name() string { return "WebSocket" }

func (l *websocketListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	c, err := chatwebhookclient.New(ctx, l.chatwebhookAddr)
	if err != nil {
		return nil, fmt.Errorf("connect to chatwebhook at %s: %w", l.chatwebhookAddr, err)
	}

	h, err := kick.NewChatHandler(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("create Kick chat handler: %w", err)
	}
	l.handler = h

	ch, err := h.GetMessagesChan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get messages channel: %w", err)
	}

	return ch, nil
}

func (l *websocketListener) Close(_ context.Context) error {
	// ChatHandler doesn't expose a Close method; it is tied to the context.
	l.handler = nil
	return nil
}

// obsoleteListener implements chathandler.ChatListener using the obsolete Kick HTTP handler.
type obsoleteListener struct {
	channelSlug string
	handler     *kick.ChatHandlerOBSOLETE
}

func (l *obsoleteListener) Name() string { return "HTTP-Obsolete" }

func (l *obsoleteListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	h, err := kick.NewChatHandlerOBSOLETE(ctx, l.channelSlug)
	if err != nil {
		return nil, fmt.Errorf("create obsolete Kick handler: %w", err)
	}
	l.handler = h

	ch, err := h.GetMessagesChan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get messages channel: %w", err)
	}

	return ch, nil
}

func (l *obsoleteListener) Close(_ context.Context) error {
	l.handler = nil
	return nil
}
