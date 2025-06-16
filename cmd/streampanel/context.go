package main

import (
	"context"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	xruntime "github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/experimental/metrics"
	prometheusadapter "github.com/facebookincubator/go-belt/tool/experimental/metrics/implementation/prometheus"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sirupsen/logrus"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/xpath"
)

var originalPCFilter xruntime.PCFilter

func init() {
	originalPCFilter = xruntime.DefaultCallerPCFilter
}

func setDefaultCallerPCFilter() {
	xruntime.DefaultCallerPCFilter = observability.CallerPCFilter(originalPCFilter)
}

func getContext(
	flags Flags,
) context.Context {
	observability.LogLevelFilter.SetLevel(logger.Level(flags.LoggerLevel))

	ctx := context.Background()
	setDefaultCallerPCFilter()

	ctx = metrics.CtxWithMetrics(ctx, prometheusadapter.Default())

	secretsProvider := observability.NewStaticSecretsProvider()
	ctx = observability.WithSecretsProvider(ctx, secretsProvider)

	ll := xlogrus.DefaultLogrusLogger()
	ll.Formatter.(*logrus.TextFormatter).ForceColors = true

	logPreHooks := logger.PreHooks{
		&observability.LogLevelFilter,
	}
	if flags.RemoveSecretsFromLogs {
		if flags.InsecureDebug {
			panic("options RemoveSecretsFromLogs and InsecureDebug contradict each other")
		}
		logPreHooks = append(logPreHooks,
			observability.StructFieldSecretsFilter{},
			observability.NewSecretValuesFilter(secretsProvider),
		)
	}

	logHooks := logger.Hooks{}
	if flags.InsecureDebug {
		secret.SetSecrecy(false)
	} else {
		logHooks = append(logHooks,
			observability.NewRemoveInsecureDebugFilter(),
		)
	}

	l := xlogrus.New(ll).WithLevel(logger.LevelTrace).WithPreHooks(logPreHooks...).WithHooks(logHooks...)

	if flags.LogFile != "" {
		logPathUnexpanded := flags.LogFile
		if flags.Subprocess != "" {
			logPathUnexpanded += "-" + flags.Subprocess
		}
		logPath, err := xpath.Expand(logPathUnexpanded)
		if err != nil {
			l.Errorf("unable to expand path '%s': %w", logPath, err)
		} else {
			var closeFile context.CancelFunc
			rotateFunc := func() {
				if closeFile != nil {
					closeFile()
					closeFile = nil
				}
				f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0750)
				if err != nil {
					l.Errorf("failed to open log file '%s': %v", logPath, err)
					return
				}
				ll.SetOutput(io.MultiWriter(os.Stderr, f))
				closeFile = func() { f.Close() }
			}
			rotateFunc()
			observability.Go(ctx, func() {
				defer func() {
					logger.Debugf(ctx, "log rotator is closed")
				}()
				t := time.NewTicker(12 * time.Hour)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						rotateFunc()
					}
				}
			})
		}
	}

	logrus.SetLevel(xlogrus.LevelToLogrus(l.Level()))

	if flags.SentryDSN != "" {
		l.Infof("setting up Sentry at DSN '%s'", flags.SentryDSN)
		sentryClient, err := sentry.NewClient(sentry.ClientOptions{
			Dsn: flags.SentryDSN,
		})
		if err != nil {
			l.Fatal(err)
		}
		sentryErrorMonitor := errmonsentry.New(sentryClient)
		ctx = errmon.CtxWithErrorMonitor(ctx, sentryErrorMonitor)
		l = l.WithPreHooks(observability.NewErrorMonitorLoggerHook(
			sentryErrorMonitor,
		))
	}

	ctx = logger.CtxWithLogger(ctx, l)

	if flags.LogstashAddr != "" {
		ctx = observability.CtxWithLogstash(
			ctx,
			flags.LogstashAddr,
			strings.ToLower(consts.AppName),
		)
	}

	ctx = belt.WithField(ctx, "program", strings.ToLower(consts.AppName))

	if hostname, err := os.Hostname(); err == nil {
		ctx = belt.WithField(ctx, "hostname", strings.ToLower(hostname))
	}

	ctx = belt.WithField(ctx, "uid", os.Getuid())
	ctx = belt.WithField(ctx, "pid", os.Getpid())

	if u, err := user.Current(); err == nil {
		ctx = belt.WithField(ctx, "user", u.Username)
	}

	if flags.RemoteAddr != "" {
		ctx = belt.WithField(ctx, "streamd_addr", flags.RemoteAddr)
	}

	l = logger.FromCtx(ctx)
	logger.Default = func() logger.Logger {
		return l
	}
	log.Logger = &zerolog.Logger{Logger: logger.FromCtx(ctx)}

	return ctx
}
