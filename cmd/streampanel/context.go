package main

import (
	"context"
	"io"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	errmonsentry "github.com/facebookincubator/go-belt/tool/experimental/errmon/implementation/sentry"
	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/getsentry/sentry-go"
	"github.com/sirupsen/logrus"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

func getContext(
	flags Flags,
) context.Context {
	ctx := context.Background()

	ll := xlogrus.DefaultLogrusLogger()
	l := xlogrus.New(ll).WithLevel(logger.Level(flags.LoggerLevel))

	if flags.LogFile != "" {
		logPath, err := xpath.Expand(flags.LogFile)
		if err != nil {
			l.Errorf("unable to expand path '%s': %w", flags.LogFile, err)
		} else {
			var closeFile context.CancelFunc
			rotateFunc := func() {
				if closeFile != nil {
					closeFile()
					closeFile = nil
				}
				f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0750)
				if err != nil {
					l.Errorf("failed to open log file '%s': %v", flags.LogFile, err)
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
			"streampanel",
			flags.SentryDSN == "",
		)
	}

	ctx = belt.WithField(ctx, "program", strings.ToLower(streampanel.AppName))

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

	return ctx
}
