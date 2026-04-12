package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/chathandler"
	"github.com/xaionaro-go/streamctl/pkg/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	// Register platform-specific ChatListenerFactory implementations.
	_ "github.com/xaionaro-go/streamctl/pkg/chathandler/platform/kick"
	_ "github.com/xaionaro-go/streamctl/pkg/chathandler/platform/twitch"
	_ "github.com/xaionaro-go/streamctl/pkg/chathandler/platform/youtube"
)

const (
	tlsProbeTimeout      = 3 * time.Second
	messagePreviewMax    = 60
	defaultOpenRouterURL = "https://openrouter.ai/api"
	defaultZenURL        = "https://api.zen.ai"
	defaultOpenAIURL     = "https://api.openai.com"
)

func main() {
	configPath := pflag.String("config", defaultConfigPath, "path to YAML config file")
	platformFlag := pflag.String("platform", "", "platform name (twitch, kick, youtube)")
	listenerTypeFlag := pflag.String("listener-type", "primary", "PACE listener type (primary, alternate, contingency, emergency)")
	streamdAddrFlag := pflag.String("streamd-addr", "", "streamd gRPC address (overrides config)")
	var logLevel logger.Level
	pflag.Var(&logLevel, "log-level", "log level")
	pflag.Parse()

	if *platformFlag == "" {
		fmt.Fprintf(os.Stderr, "fatal: --platform is required\n")
		os.Exit(1)
	}

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

	if *streamdAddrFlag != "" {
		cfg.StreamdAddr = *streamdAddrFlag
	}

	platName := streamcontrol.PlatformName(*platformFlag)

	listenerType, err := streamcontrol.ChatListenerTypeFromString(*listenerTypeFlag)
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

	if err := run(ctx, cfg, platName, listenerType, chain); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(
	ctx context.Context,
	cfg AppConfig,
	platName streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	chain *llm.TranslatorChain,
) (_err error) {
	logger.Tracef(ctx, "run")
	defer func() { logger.Tracef(ctx, "/run: %v", _err) }()

	if cfg.StreamdAddr == "" {
		return fmt.Errorf("streamd_addr is required (set in config or via --streamd-addr)")
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

	factory := chathandler.GetChatListenerFactory(platName)
	if factory == nil {
		return fmt.Errorf("no ChatListenerFactory registered for platform %q", platName)
	}

	platCfg, err := buildPlatformConfig(ctx, cfg, sdClient, platName)
	if err != nil {
		return fmt.Errorf("build platform config for %s: %w", platName, err)
	}

	listener, err := factory.CreateChatListener(ctx, platCfg, listenerType)
	if err != nil {
		return fmt.Errorf("create %s listener (%s): %w", platName, listenerType, err)
	}

	source := &ChatSourceFromListener{
		Listener:     listener,
		PlatformName: platName,
	}

	eng := &Engine{
		StreamdClient: sdClient,
		Chain:         chain,
	}

	events := make(chan ChatEvent, 64)

	var sourceErr error
	done := make(chan struct{})
	observability.Go(ctx, func(ctx context.Context) {
		defer close(done)
		logger.Debugf(ctx, "starting source: %s (%s)", platName, listenerType)
		sourceErr = source.Run(ctx, events)
		close(events)
	})

	engineErr := eng.Run(ctx, events)
	<-done
	return errors.Join(sourceErr, engineErr)
}

// buildPlatformConfig constructs the AbstractPlatformConfig for the given
// platform. It first tries FetchPlatformConfig from streamd. If that fails
// or credentials are overridden in the local config, it uses the local
// config values.
func buildPlatformConfig(
	ctx context.Context,
	cfg AppConfig,
	sdClient streamd_grpc.StreamDClient,
	platName streamcontrol.PlatformName,
) (_ *streamcontrol.AbstractPlatformConfig, _err error) {
	logger.Tracef(ctx, "buildPlatformConfig[%s]", platName)
	defer func() { logger.Tracef(ctx, "/buildPlatformConfig[%s]: %v", platName, _err) }()

	// Try fetching from streamd first.
	platCfg, err := chathandler.FetchPlatformConfig(ctx, sdClient, platName)
	if err != nil {
		logger.Debugf(ctx, "FetchPlatformConfig failed for %s, using local config: %v", platName, err)
	}

	// Apply local overrides from config file.
	localOverride, hasOverride := cfg.PlatformOverrides[string(platName)]
	if hasOverride {
		if platCfg == nil {
			platCfg = &streamcontrol.AbstractPlatformConfig{}
		}
		platCfg = localOverride.applyTo(platCfg)
	}

	if platCfg == nil {
		return nil, fmt.Errorf("no config available for platform %s (not in streamd, no local override)", platName)
	}

	return platCfg, nil
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
