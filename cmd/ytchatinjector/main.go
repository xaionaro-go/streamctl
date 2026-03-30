package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Config holds all runtime configuration for the chat bridge.
type Config struct {
	YTProxyAddr   string
	StreamdAddr   string
	Video         string
	Channel       string
	Hl            string
	UseRawMessage bool
	Translator    *Translator
}

const (
	platformIDYouTube = "youtube"
	reconnectDelay    = 5 * time.Second
)

func main() {
	ytProxyAddr := pflag.String("yt-proxy-addr", "", "youtubeapiproxy gRPC address (host:port)")
	streamdAddr := pflag.String("streamd-addr", "", "streamd gRPC address (host:port)")
	video := pflag.String("video", "", "video URL, video ID, or liveChatId")
	channel := pflag.String("channel", "", "channel URL, @handle, or channel ID to monitor for live streams")
	hl := pflag.String("hl", "", "language for YouTube system messages (e.g. en, de, ja)")
	useRawMessage := pflag.Bool("raw-message", false, "use TextMessageDetails instead of DisplayMessage")
	translateTo := pflag.String("translate-to", "", "translate messages to this language using LLM (e.g. en, ru, ja)")
	llmProvider := pflag.String("llm-provider", "http://192.168.0.171:11434", "LLM provider: Ollama URL, or streamdcfg:/path or streampanelcfg:/path")
	llmModel := pflag.String("llm-model", llmDefaultModel, "LLM model for translation")
	llmAPIKey := pflag.String("llm-api-key", "", "API key for LLM provider (overrides config)")
	var logLevel logger.Level
	pflag.Var(&logLevel, "log-level", "log level")
	pflag.Parse()

	if logLevel == logger.LevelUndefined {
		logLevel = logger.LevelDebug
	}
	observability.LogLevelFilter.SetLevel(logLevel)

	l := logrus.Default()
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx = belt.CtxWithBelt(ctx, belt.New())
	ctx = logger.CtxWithLogger(ctx, l.WithLevel(logLevel))
	defer belt.Flush(ctx)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	cfg := Config{
		YTProxyAddr:   *ytProxyAddr,
		StreamdAddr:   *streamdAddr,
		Video:         *video,
		Channel:       *channel,
		Hl:            *hl,
		UseRawMessage: *useRawMessage,
	}
	if *translateTo != "" {
		resolved, err := resolveLLMConfig(ctx, *llmProvider, *llmModel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
		apiKey := resolved.APIKey
		if *llmAPIKey != "" {
			apiKey = *llmAPIKey
		}
		cfg.Translator = &Translator{
			APIURL:     resolved.APIURL,
			APIKey:     apiKey,
			Model:      resolved.Model,
			TargetLang: *translateTo,
		}
	}

	if err := run(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	cfg Config,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	switch {
	case cfg.YTProxyAddr == "":
		return fmt.Errorf("--yt-proxy-addr is required")
	case cfg.StreamdAddr == "":
		return fmt.Errorf("--streamd-addr is required")
	case cfg.Video == "" && cfg.Channel == "":
		return fmt.Errorf("either --video or --channel is required")
	case cfg.Video != "" && cfg.Channel != "":
		return fmt.Errorf("--video and --channel are mutually exclusive")
	}

	ytConn, err := grpc.NewClient(cfg.YTProxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("connect to youtubeapiproxy at %s: %w", cfg.YTProxyAddr, err)
	}
	defer ytConn.Close()

	sdCreds := detectTransportCredentials(ctx, cfg.StreamdAddr)
	sdConn, err := grpc.NewClient(cfg.StreamdAddr,
		grpc.WithTransportCredentials(sdCreds),
	)
	if err != nil {
		return fmt.Errorf("connect to streamd at %s: %w", cfg.StreamdAddr, err)
	}
	defer sdConn.Close()

	ytClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(ytConn)
	sdClient := streamd_grpc.NewStreamDClient(sdConn)

	if cfg.Channel != "" {
		return monitorChannel(ctx, ytConn, cfg.Channel, func(ctx context.Context, liveChatID string) error {
			return bridgeLoop(ctx, ytClient, sdClient, liveChatID, cfg)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, cfg.Video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", cfg.Video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return bridgeLoop(ctx, ytClient, sdClient, liveChatID, cfg)
}

// resolveLiveChatID determines the liveChatId from the user-provided target.
// If the target looks like a video URL or video ID, it calls ResolveLiveChatId
// on the admin service. Otherwise it assumes the target is already a liveChatId.
func resolveLiveChatID(
	ctx context.Context,
	conn grpc.ClientConnInterface,
	target string,
) (_ret string, _err error) {
	logger.Tracef(ctx, "resolveLiveChatID")
	defer func() { logger.Tracef(ctx, "/resolveLiveChatID: %v", _err) }()

	switch {
	case strings.Contains(target, "youtube.com"),
		strings.Contains(target, "youtu.be"),
		strings.Contains(target, "://"):
		// URL — resolve via admin RPC.
	case len(target) == 11:
		// Likely a video ID (YouTube video IDs are 11 chars).
	default:
		return target, nil
	}

	adminClient := ytgrpc.NewAdminServiceClient(conn)
	resp, err := adminClient.ResolveLiveChatId(ctx, &ytgrpc.ResolveLiveChatIdRequest{
		VideoId: target,
	})
	if err != nil {
		return "", fmt.Errorf("ResolveLiveChatId RPC: %w", err)
	}

	return resp.LiveChatId, nil
}

func bridgeLoop(
	ctx context.Context,
	ytClient ytgrpc.V3DataLiveChatMessageServiceClient,
	sdClient streamd_grpc.StreamDClient,
	liveChatID string,
	cfg Config,
) (_err error) {
	logger.Tracef(ctx, "bridgeLoop")
	defer func() { logger.Tracef(ctx, "/bridgeLoop: %v", _err) }()

	var nextPageToken string

	for {
		err := streamOnce(ctx, ytClient, sdClient, liveChatID, cfg, &nextPageToken)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			logger.Debugf(ctx, "stream ended, reconnecting...")
		default:
			logger.Warnf(ctx, "stream error, retrying in %v: %v", reconnectDelay, err)
		}

		if !sleep(ctx, reconnectDelay) {
			return ctx.Err()
		}
	}
}

func streamOnce(
	ctx context.Context,
	ytClient ytgrpc.V3DataLiveChatMessageServiceClient,
	sdClient streamd_grpc.StreamDClient,
	liveChatID string,
	cfg Config,
	nextPageToken *string,
) (_err error) {
	logger.Tracef(ctx, "streamOnce")
	defer func() { logger.Tracef(ctx, "/streamOnce: %v", _err) }()

	req := &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Hl:         cfg.Hl,
		Part:       []string{"snippet", "authorDetails"},
		PageToken:  *nextPageToken,
	}

	stream, err := ytClient.StreamList(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to start StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("stream recv: %w", err)
		}

		if token := resp.GetNextPageToken(); token != "" {
			*nextPageToken = token
		}

		for _, msg := range resp.GetItems() {
			if err := injectMessage(ctx, sdClient, msg, cfg); err != nil {
				logger.Errorf(ctx, "unable to inject message %s: %v", msg.GetId(), err)
			}
		}
	}
}

func injectMessage(
	ctx context.Context,
	sdClient streamd_grpc.StreamDClient,
	msg *ytgrpc.LiveChatMessage,
	cfg Config,
) error {
	ev, err := convertLiveChatMessage(msg, cfg.UseRawMessage)
	if err != nil {
		return fmt.Errorf("unable to convert message: %w", err)
	}

	if cfg.Translator != nil {
		if content := ev.GetMessage().GetContent(); content != "" {
			userName := ev.GetUser().GetName()
			translated, translateErr := cfg.Translator.Translate(ctx, userName, content)
			switch {
			case translateErr != nil:
				logger.Warnf(ctx, "translation failed for %s: %v", msg.GetId(), translateErr)
			case translated != content:
				logger.Debugf(ctx, "translated [%s]: %q -> %q", userName, content, translated)
				ev.Message.Content = translated
			}
		}
	}

	logger.Debugf(ctx, "injecting message id=%s user=%s: %s",
		ev.GetId(), ev.GetUser().GetName(), ev.GetMessage().GetContent())

	_, err = sdClient.InjectPlatformEvent(ctx, &streamd_grpc.InjectPlatformEventRequest{
		PlatID:       platformIDYouTube,
		IsLive:       true,
		IsPersistent: true,
		Message:      ev,
	})
	if err != nil {
		return fmt.Errorf("InjectPlatformEvent: %w", err)
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

const tlsProbeTimeout = 3 * time.Second

// detectTransportCredentials probes the server with a TLS handshake.
// If the handshake succeeds, it returns TLS credentials (with InsecureSkipVerify
// for self-signed certs). Otherwise it returns insecure plaintext credentials.
func detectTransportCredentials(
	ctx context.Context,
	addr string,
) credentials.TransportCredentials {
	probeCtx, cancel := context.WithTimeout(ctx, tlsProbeTimeout)
	defer cancel()

	dialer := tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	conn, err := dialer.DialContext(probeCtx, "tcp", addr)
	if err != nil {
		logger.Debugf(ctx, "TLS probe to %s failed, using plaintext: %v", addr, err)
		return insecure.NewCredentials()
	}
	conn.Close()

	logger.Debugf(ctx, "TLS probe to %s succeeded, using TLS", addr)
	return credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	})
}
