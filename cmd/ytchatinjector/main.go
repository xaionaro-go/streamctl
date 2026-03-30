package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	scgoconv "github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	reconnectDelay  = 5 * time.Second
	tlsProbeTimeout = 3 * time.Second
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

	chatClient := ytgrpc.NewV3DataLiveChatMessageServiceClient(ytConn)
	streamdClient := streamd_grpc.NewStreamDClient(sdConn)

	if cfg.Channel != "" {
		return monitorChannel(ctx, ytConn, cfg.Channel, func(ctx context.Context, liveChatID string) error {
			return bridgeChat(ctx, chatClient, streamdClient, liveChatID, cfg)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, cfg.Video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", cfg.Video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return bridgeChat(ctx, chatClient, streamdClient, liveChatID, cfg)
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

func bridgeChat(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	streamdClient streamd_grpc.StreamDClient,
	liveChatID string,
	cfg Config,
) (_err error) {
	logger.Tracef(ctx, "bridgeChat")
	defer func() { logger.Tracef(ctx, "/bridgeChat: %v", _err) }()

	var pageToken string

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := recvAndInject(ctx, chatClient, streamdClient, liveChatID, cfg, &pageToken)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			logger.Debugf(ctx, "stream ended, reconnecting...")
		default:
			logger.Warnf(ctx, "stream error, reconnecting in %s: %v", reconnectDelay, err)
		}

		if !sleep(ctx, reconnectDelay) {
			return ctx.Err()
		}
	}
}

func recvAndInject(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	streamdClient streamd_grpc.StreamDClient,
	liveChatID string,
	cfg Config,
	pageToken *string,
) (_err error) {
	logger.Tracef(ctx, "recvAndInject")
	defer func() { logger.Tracef(ctx, "/recvAndInject: %v", _err) }()

	stream, err := chatClient.StreamList(ctx, &ytgrpc.LiveChatMessageListRequest{
		LiveChatId: liveChatID,
		Hl:         cfg.Hl,
		Part:       []string{"snippet", "authorDetails"},
		PageToken:  *pageToken,
	})
	if err != nil {
		return fmt.Errorf("StreamList: %w", err)
	}

	for {
		resp, err := stream.Recv()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case err != nil:
			return fmt.Errorf("stream recv: %w", err)
		}

		if resp.NextPageToken != "" {
			*pageToken = resp.NextPageToken
		}

		for _, item := range resp.Items {
			ev := convertMessage(ctx, item, cfg.UseRawMessage)

			if cfg.Translator != nil && ev.Message != nil && ev.Message.Content != "" {
				translated, translateErr := cfg.Translator.Translate(ctx, ev.User.Name, ev.Message.Content)
				switch {
				case translateErr != nil:
					logger.Warnf(ctx, "translation failed for %s: %v", item.Id, translateErr)
				case translated != ev.Message.Content:
					logger.Debugf(ctx, "translated [%s]: %q -> %q", ev.User.Name, ev.Message.Content, translated)
					ev.Message.Content = translated
				}
			}

			logger.Debugf(ctx, "injecting %s event from %s: %s",
				ev.Type, ev.User.Name, messagePreview(&ev))

			_, injectErr := streamdClient.InjectChatMessage(ctx, &streamd_grpc.InjectChatMessageRequest{
				PlatID: string(youtube.ID),
				Event:  scgoconv.EventGo2GRPC(ev),
			})
			if injectErr != nil {
				logger.Errorf(ctx, "InjectChatMessage failed for %s: %v", item.Id, injectErr)
			}
		}
	}
}

func messagePreview(ev *streamcontrol.Event) string {
	if ev.Message == nil {
		return fmt.Sprintf("(%s)", ev.Type)
	}
	content := ev.Message.Content
	const maxLen = 60
	if len(content) > maxLen {
		content = content[:maxLen] + "..."
	}
	return content
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
