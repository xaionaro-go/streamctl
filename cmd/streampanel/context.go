package main

import (
	"context"
	"os"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	"github.com/sirupsen/logrus"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

func getContext(flags Flags) context.Context {
	ctx := context.Background()

	ll := xlogrus.DefaultLogrusLogger()
	l := xlogrus.New(ll).WithLevel(flags.LoggerLevel)

	if flags.LogFile != "" {
		f, err := os.OpenFile(flags.LogFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0750)
		if err != nil {
			l.Errorf("failed to open log file '%s': %v", flags.LogFile, err)
		}
		ll.SetOutput(f)
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
	logger.Default = func() logger.Logger {
		return l
	}

	return ctx
}
