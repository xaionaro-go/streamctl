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
	"github.com/goccy/go-yaml"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/chatwebhook/pkg/grpc/protobuf/go/chatwebhook_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	llmcfg "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/youtubeapiproxy/grpc/ytgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	reconnectDelay       = 5 * time.Second
	tlsProbeTimeout      = 3 * time.Second
	messagePreviewMax    = 60
	defaultOpenRouterURL = "https://openrouter.ai/api"
	defaultZenURL        = "https://api.zen.ai"
	defaultOpenAIURL     = "https://api.openai.com"
	translationSeparator = " -文A-> "
)

func main() {
	configPath := pflag.String("config", defaultConfigPath, "path to YAML config file")
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

	cfg, err := loadConfig(ctx, *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}

	var chain *llm.TranslatorChain
	if cfg.Translation.TargetLanguage != "" {
		chain, err = buildTranslatorChain(ctx, cfg.Translation)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
	}

	if err := run(ctx, cfg, chain); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func buildTranslatorChain(
	ctx context.Context,
	tc TranslationConfig,
) (_ret *llm.TranslatorChain, _err error) {
	logger.Tracef(ctx, "buildTranslatorChain")
	defer func() { logger.Tracef(ctx, "/buildTranslatorChain: %v", _err) }()

	var entries []llm.ProviderEntry

	for _, pc := range tc.Providers {
		expanded, err := expandProvider(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("create provider %q: %w", pc.Type, err)
		}
		entries = append(entries, expanded...)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("translation target_language is set but no providers configured")
	}

	logger.Debugf(ctx, "built translator chain with %d providers for language %q",
		len(entries), tc.TargetLanguage)

	return llm.NewTranslatorChain(tc.TargetLanguage, tc.ChatHistorySize, entries), nil
}

func expandProvider(
	ctx context.Context,
	pc ProviderConfig,
) (_ret []llm.ProviderEntry, _err error) {
	logger.Tracef(ctx, "expandProvider")
	defer func() { logger.Tracef(ctx, "/expandProvider: %v", _err) }()

	entry := func(p llm.Provider) []llm.ProviderEntry {
		return []llm.ProviderEntry{{
			Provider:                p,
			Parallelism:             pc.Parallelism,
			MaxQueueSize:            pc.MaxQueueSize,
			Timeout:                 pc.Timeout,
			CircuitBreakerThreshold: pc.CircuitBreakerThreshold,
			CircuitBreakerCooldown:  pc.CircuitBreakerCooldown,
		}}
	}

	switch pc.Type {
	case "ollama":
		return entry(&llm.OllamaProvider{APIURL: pc.APIURL, Model: pc.Model}), nil
	case "openai":
		return entry(&llm.OpenAIProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "openrouter":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultOpenRouterURL
		}
		return entry(&llm.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "zen":
		apiURL := pc.APIURL
		if apiURL == "" {
			apiURL = defaultZenURL
		}
		return entry(&llm.OpenAIProvider{APIURL: apiURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "anthropic":
		return entry(&llm.AnthropicProvider{APIURL: pc.APIURL, APIKey: pc.APIKey, Model: pc.Model}), nil
	case "claude-code":
		return entry(&llm.ClaudeCodeProvider{Model: pc.Model, Effort: pc.Effort}), nil
	case "streamdcfg", "streampanelcfg":
		return importLLMProviders(ctx, pc.ConfigPath, pc.Parallelism, pc.Timeout)
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}

// importLLMProviders reads LLM endpoints from a streamd or streampanel
// config file and converts them into provider instances.
func importLLMProviders(
	ctx context.Context,
	cfgPath string,
	defaultParallelism int,
	defaultTimeout time.Duration,
) (_ret []llm.ProviderEntry, _err error) {
	logger.Tracef(ctx, "importLLMProviders")
	defer func() { logger.Tracef(ctx, "/importLLMProviders: %v", _err) }()

	expandedPath, err := xpath.Expand(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("expand path %q: %w", cfgPath, err)
	}

	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", expandedPath, err)
	}

	// Minimal struct to extract LLM config from either streamd or streampanel format.
	type configLLMNested struct {
		LLM llmcfg.Config `yaml:"llm"`
	}
	type configLLMOnly struct {
		LLM            llmcfg.Config   `yaml:"llm"`
		BuiltinStreamD configLLMNested `yaml:"streamd_builtin"`
	}

	var cfg configLLMOnly
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %q: %w", expandedPath, err)
	}

	// Merge endpoints from both top-level (streamd) and nested (streampanel) locations.
	allEndpoints := llmcfg.Endpoints{}
	for k, v := range cfg.LLM.Endpoints {
		allEndpoints[k] = v
	}
	for k, v := range cfg.BuiltinStreamD.LLM.Endpoints {
		allEndpoints[k] = v
	}

	logger.Debugf(ctx, "imported %d LLM endpoints from %q", len(allEndpoints), expandedPath)

	par := defaultParallelism
	if par <= 0 {
		par = 1
	}

	var result []llm.ProviderEntry
	for name, endpoint := range allEndpoints {
		if endpoint == nil {
			continue
		}

		logger.Debugf(ctx, "  endpoint %q: provider=%q api_url=%q model=%q",
			name, endpoint.Provider, endpoint.APIURL, endpoint.ModelName)

		p, err := endpointToProvider(endpoint)
		if err != nil {
			logger.Warnf(ctx, "skipping endpoint %q: %v", name, err)
			continue
		}

		result = append(result, llm.ProviderEntry{Provider: p, Parallelism: par, Timeout: defaultTimeout})
	}

	return result, nil
}

func endpointToProvider(endpoint *llmcfg.Endpoint) (llm.Provider, error) {
	switch endpoint.Provider {
	case llmcfg.ProviderChatGPT:
		apiURL := endpoint.APIURL
		if apiURL == "" {
			apiURL = defaultOpenAIURL
		}
		return &llm.OpenAIProvider{
			APIURL: apiURL,
			APIKey: endpoint.APIKey,
			Model:  endpoint.ModelName,
		}, nil
	default:
		if endpoint.APIURL == "" {
			return nil, fmt.Errorf("unsupported provider %q with no api_url", endpoint.Provider)
		}
		// Treat unknown providers with a URL as Ollama-compatible.
		return &llm.OllamaProvider{
			APIURL: endpoint.APIURL,
			Model:  endpoint.ModelName,
		}, nil
	}
}

func run(
	ctx context.Context,
	cfg AppConfig,
	chain *llm.TranslatorChain,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	switch {
	case cfg.YTProxyAddr == "":
		return fmt.Errorf("yt_proxy_addr is required")
	case cfg.StreamdAddr == "":
		return fmt.Errorf("streamd_addr is required")
	case cfg.Video == "" && cfg.Channel == "":
		return fmt.Errorf("either video or channel is required")
	case cfg.Video != "" && cfg.Channel != "":
		return fmt.Errorf("video and channel are mutually exclusive")
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

	detectMethod := DetectMethod(cfg.DetectMethod)
	if detectMethod == "" {
		detectMethod = DetectMethodSearch
	}

	if cfg.Channel != "" {
		return monitorChannel(ctx, ytConn, cfg.Channel, detectMethod, func(ctx context.Context, liveChatID string) error {
			return bridgeChat(ctx, chatClient, streamdClient, liveChatID, cfg, chain)
		})
	}

	liveChatID, err := resolveLiveChatID(ctx, ytConn, cfg.Video)
	if err != nil {
		return fmt.Errorf("resolve live chat ID for %q: %w", cfg.Video, err)
	}
	logger.Infof(ctx, "resolved live chat ID: %s", liveChatID)

	return bridgeChat(ctx, chatClient, streamdClient, liveChatID, cfg, chain)
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
		// URL -- resolve via admin RPC.
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
	cfg AppConfig,
	chain *llm.TranslatorChain,
) (_err error) {
	logger.Tracef(ctx, "bridgeChat")
	defer func() { logger.Tracef(ctx, "/bridgeChat: %v", _err) }()

	var pageToken string

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err := recvAndInject(ctx, chatClient, streamdClient, liveChatID, cfg, chain, &pageToken)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		switch {
		case err == nil:
			logger.Debugf(ctx, "stream ended, reconnecting immediately")
			continue
		case isStreamEndedError(err):
			logger.Infof(ctx, "live chat ended: %v", err)
			return nil
		default:
			logger.Warnf(ctx, "stream error, reconnecting in %s: %v", reconnectDelay, err)
			if !sleep(ctx, reconnectDelay) {
				return ctx.Err()
			}
		}
	}
}

func recvAndInject(
	ctx context.Context,
	chatClient ytgrpc.V3DataLiveChatMessageServiceClient,
	streamdClient streamd_grpc.StreamDClient,
	liveChatID string,
	cfg AppConfig,
	chain *llm.TranslatorChain,
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

		// Translate concurrently, inject in order.
		// Each message gets a done channel; a sequential loop
		// waits for each in order and injects immediately.
		type result struct {
			ev   streamcontrol.Event
			item *ytgrpc.LiveChatMessage
		}
		slots := make([]chan result, len(resp.Items))

		for i, item := range resp.Items {
			if s := item.GetSnippet(); s != nil {
				logger.Debugf(ctx, "received yt event: id=%s type=%s user=%s msg=%q",
					item.GetId(), s.GetType(), item.GetAuthorDetails().GetDisplayName(),
					s.GetDisplayMessage())
			}
			slots[i] = make(chan result, 1)
			i, item := i, item
			observability.Go(ctx, func(ctx context.Context) {
				// Guarantee the slot is always filled, even on panic.
				sent := false
				defer func() {
					if !sent {
						ev := convertMessage(ctx, item, cfg.RawMessage)
						slots[i] <- result{ev: ev, item: item}
					}
				}()

				ev := convertMessage(ctx, item, cfg.RawMessage)

				if chain != nil && ev.Message != nil && ev.Message.Content != "" {
					translated, translateErr := chain.Translate(ctx, ev.User.Name, ev.Message.Content)
					switch {
					case translateErr != nil:
						logger.Warnf(ctx, "translation failed for %s: %v", item.Id, translateErr)
					case translated != ev.Message.Content:
						ev.Message.Content = ev.Message.Content + translationSeparator + translated
					}
				}

				slots[i] <- result{ev: ev, item: item}
				sent = true
			})
		}

		for _, slot := range slots {
			r := <-slot
			logger.Debugf(ctx, "injecting %s event from %s: %s",
				r.ev.Type, r.ev.User.Name, messagePreview(&r.ev))

			grpcEv := eventToGRPC(r.ev)
			_, injectErr := streamdClient.InjectPlatformEvent(ctx, &streamd_grpc.InjectPlatformEventRequest{
				PlatID:       string(youtube.ID),
				IsLive:       true,
				IsPersistent: true,
				Message:      grpcEv,
			})
			if injectErr != nil {
				logger.Errorf(ctx, "InjectPlatformEvent failed for %s: %v", r.item.Id, injectErr)
			}
		}
	}
}

// eventToGRPC converts a streamcontrol.Event into the chatwebhook_grpc.Event
// used by the drafts-branch InjectPlatformEvent RPC.
func eventToGRPC(ev streamcontrol.Event) *chatwebhook_grpc.Event {
	grpcEv := &chatwebhook_grpc.Event{
		Id:                string(ev.ID),
		CreatedAtUNIXNano: uint64(clock.Get().Now().UnixNano()),
		EventType:         eventTypeToGRPC(ev.Type),
		User: &chatwebhook_grpc.User{
			Id:   string(ev.User.ID),
			Slug: ev.User.Slug,
			Name: ev.User.Name,
		},
	}

	if ev.Message != nil {
		grpcEv.Message = &chatwebhook_grpc.Message{
			Content:    ev.Message.Content,
			FormatType: chatwebhook_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN,
		}
	}

	return grpcEv
}

func eventTypeToGRPC(t streamcontrol.EventType) chatwebhook_grpc.PlatformEventType {
	switch t {
	case streamcontrol.EventTypeChatMessage:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeChatMessage
	case streamcontrol.EventTypeSubscriptionNew:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeSubscriptionNew
	case streamcontrol.EventTypeSubscriptionRenewed:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeSubscriptionRenewed
	case streamcontrol.EventTypeGiftedSubscription:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeGiftedSubscription
	case streamcontrol.EventTypeBan:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeBan
	case streamcontrol.EventTypeStreamOffline:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeStreamOffline
	default:
		return chatwebhook_grpc.PlatformEventType_platformEventTypeUndefined
	}
}

func messagePreview(ev *streamcontrol.Event) string {
	if ev.Message == nil {
		return fmt.Sprintf("(%s)", ev.Type)
	}

	content := ev.Message.Content
	if len(content) > messagePreviewMax {
		content = content[:messagePreviewMax] + "..."
	}

	return content
}

func isStreamEndedError(err error) bool {
	s := err.Error()
	switch {
	case strings.Contains(s, "FailedPrecondition"):
		return true
	case strings.Contains(s, "liveChatEnded"):
		return true
	case strings.Contains(s, "liveChatNotFound"):
		return true
	case strings.Contains(s, "liveChatDisabled"):
		return true
	default:
		return false
	}
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
) (_ret credentials.TransportCredentials) {
	logger.Tracef(ctx, "detectTransportCredentials")
	defer func() { logger.Tracef(ctx, "/detectTransportCredentials") }()

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
