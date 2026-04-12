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
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	youtubetypes "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/api/option"
	youtubesvc "google.golang.org/api/youtube/v3"
)

func main() {
	streamdAddr := pflag.String("streamd-addr", "localhost:3594", "streamd gRPC address")
	apiKey := pflag.String("api-key", "", "YouTube Data API v3 key (required for polling mode)")
	liveChatID := pflag.String("live-chat-id", "", "YouTube live chat ID (required)")
	videoID := pflag.String("video-id", "", "YouTube video ID (for reference)")
	ytProxyAddr := pflag.String("yt-proxy-addr", "", "youtubeapiproxy gRPC address (enables gRPC streaming mode)")
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

	if *liveChatID == "" {
		fmt.Fprintf(os.Stderr, "fatal: --live-chat-id is required\n")
		os.Exit(1)
	}
	if *ytProxyAddr == "" && *apiKey == "" {
		fmt.Fprintf(os.Stderr, "fatal: either --api-key or --yt-proxy-addr is required\n")
		os.Exit(1)
	}

	cfg := youtubeHandlerConfig{
		streamdAddr: *streamdAddr,
		apiKey:      *apiKey,
		liveChatID:  *liveChatID,
		videoID:     *videoID,
		ytProxyAddr: *ytProxyAddr,
	}

	if err := run(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

type youtubeHandlerConfig struct {
	streamdAddr string
	apiKey      string
	liveChatID  string
	videoID     string
	ytProxyAddr string
}

func run(
	ctx context.Context,
	cfg youtubeHandlerConfig,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	creds := chathandler.DetectTransportCredentials(ctx, cfg.streamdAddr)
	conn, streamdClient, err := chathandler.ConnectToStreamd(ctx, cfg.streamdAddr, creds)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Disable built-in chat listener for YouTube since we're taking over.
	_, err = streamdClient.SetBuiltinChatListenerEnabled(ctx, &streamd_grpc.SetBuiltinChatListenerEnabledRequest{
		PlatID:  string(youtubetypes.ID),
		Enabled: false,
	})
	if err != nil {
		logger.Warnf(ctx, "failed to disable built-in YouTube chat listener: %v", err)
	}

	var primary chathandler.ChatListener
	var fallback chathandler.ChatListener

	switch {
	case cfg.ytProxyAddr != "" && cfg.apiKey != "":
		// youtubeapiproxy gRPC streaming as primary, direct API polling as fallback.
		primary = &grpcStreamListener{
			ytProxyAddr: cfg.ytProxyAddr,
			liveChatID:  cfg.liveChatID,
		}
		fallback = &pollingListener{
			apiKey:     cfg.apiKey,
			liveChatID: cfg.liveChatID,
			videoID:    cfg.videoID,
		}
	case cfg.ytProxyAddr != "":
		// youtubeapiproxy only — both primary and fallback use gRPC streaming.
		primary = &grpcStreamListener{
			ytProxyAddr: cfg.ytProxyAddr,
			liveChatID:  cfg.liveChatID,
		}
		fallback = &grpcStreamListener{
			ytProxyAddr: cfg.ytProxyAddr,
			liveChatID:  cfg.liveChatID,
		}
	default:
		// Direct API polling only.
		primary = &pollingListener{
			apiKey:     cfg.apiKey,
			liveChatID: cfg.liveChatID,
			videoID:    cfg.videoID,
		}
		fallback = &pollingListener{
			apiKey:     cfg.apiKey,
			liveChatID: cfg.liveChatID,
			videoID:    cfg.videoID,
		}
	}

	runner := chathandler.NewRunner(
		youtubetypes.ID,
		streamdClient,
		primary,
		fallback,
		chathandler.RunnerConfig{},
	)

	return runner.Run(ctx)
}

// apiKeyChatClient wraps a youtube.Service to implement youtube.ChatClient.
type apiKeyChatClient struct {
	service *youtubesvc.Service
}

func (c *apiKeyChatClient) GetLiveChatMessages(
	ctx context.Context,
	chatID string,
	pageToken string,
	parts []string,
) (*youtubesvc.LiveChatMessageListResponse, error) {
	q := c.service.LiveChatMessages.List(chatID, parts).Context(ctx)
	if pageToken != "" {
		q = q.PageToken(pageToken)
	}
	return q.Do()
}

// pollingListener implements chathandler.ChatListener using YouTube Data API v3 polling.
type pollingListener struct {
	apiKey     string
	liveChatID string
	videoID    string
	listener   *youtube.ChatListener
}

func (l *pollingListener) Name() string { return "YouTube-Polling" }

func (l *pollingListener) Listen(
	ctx context.Context,
) (<-chan streamcontrol.Event, error) {
	svc, err := youtubesvc.NewService(ctx, option.WithAPIKey(l.apiKey))
	if err != nil {
		return nil, fmt.Errorf("create YouTube service: %w", err)
	}

	client := &apiKeyChatClient{service: svc}

	listener, err := youtube.NewChatListener(ctx, client, l.videoID, l.liveChatID)
	if err != nil {
		return nil, fmt.Errorf("create chat listener: %w", err)
	}
	l.listener = listener

	return listener.MessagesChan(), nil
}

func (l *pollingListener) Close(ctx context.Context) error {
	if l.listener != nil {
		return l.listener.Close(ctx)
	}
	return nil
}
