package main

import (
	"context"
	"io"
	"os"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	"github.com/sirupsen/logrus"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

func getContext(flags Flags) context.Context {
	ctx := context.Background()

	ll := xlogrus.DefaultLogrusLogger()
	l := xlogrus.New(ll).WithLevel(flags.LoggerLevel)

	if flags.LogFile != "" {
		logPath, err := xpath.Expand(flags.LogFile)
		if err != nil {
			l.Errorf("unable to expand path '%s': %w", flags.LogFile, err)
		} else {
			f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0750)
			if err != nil {
				l.Errorf("failed to open log file '%s': %v", flags.LogFile, err)
			}
			ll.SetOutput(io.MultiWriter(os.Stderr, f))
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
		ctx = observability.CtxWithLogstash(ctx, flags.LogstashAddr, "streampanel")
	}

	l = logger.FromCtx(ctx)
	logger.Default = func() logger.Logger {
		return l
	}

	return ctx
}
