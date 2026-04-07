package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/goccy/go-yaml"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/llm"
	llmcfg "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/xpath"
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
	case cfg.StreamdAddr == "":
		return fmt.Errorf("streamd_addr is required")
	case len(cfg.Platforms) == 0:
		return fmt.Errorf("at least one platform is required")
	}

	sdCreds := detectTransportCredentials(ctx, cfg.StreamdAddr)
	sdConn, err := grpc.NewClient(cfg.StreamdAddr,
		grpc.WithTransportCredentials(sdCreds),
	)
	if err != nil {
		return fmt.Errorf("connect to streamd at %s: %w", cfg.StreamdAddr, err)
	}
	defer sdConn.Close()

	sdClient := streamd_grpc.NewStreamDClient(sdConn)

	for i := range cfg.Platforms {
		if err := cfg.Platforms[i].resolveCredentials(ctx, sdClient); err != nil {
			return fmt.Errorf("resolve credentials for platform %q: %w", cfg.Platforms[i].Type, err)
		}
	}

	eng := &Engine{
		StreamdClient: sdClient,
		Chain:         chain,
	}

	events := make(chan ChatEvent, 64)

	var (
		wg        sync.WaitGroup
		sourceErr error
		mu        sync.Mutex
	)

	for _, pc := range cfg.Platforms {
		source, newErr := pc.NewSource()
		if newErr != nil {
			return fmt.Errorf("create source for platform %q: %w", pc.Type, newErr)
		}

		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			logger.Debugf(ctx, "starting source: %s", source.PlatformID())
			if runErr := source.Run(ctx, events); runErr != nil {
				mu.Lock()
				sourceErr = errors.Join(sourceErr, runErr)
				mu.Unlock()
			}
		})
	}

	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(events)
	})

	engineErr := eng.Run(ctx, events)
	return errors.Join(sourceErr, engineErr)
}

// isStreamEndedError returns true if the error indicates the live chat
// no longer exists (stream ended, chat disabled, etc.).
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
