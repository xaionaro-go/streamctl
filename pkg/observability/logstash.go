package observability

import (
	"context"
	"net/url"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/logrustash"
)

func CtxWithLogstash(
	ctx context.Context,
	logstashAddr string,
	appName string,
) context.Context {
	addr, err := url.Parse(logstashAddr)
	if err != nil {
		logger.Errorf(ctx, "unable to parse '%s' as URL: %v", logstashAddr, err)
		return ctx
	}

	hook, err := logrustash.NewHook(addr.Scheme, addr.Host, appName)
	if err != nil {
		logger.Errorf(ctx, "unable to initialize the hook: %v", err)
		return ctx
	}

	l := logger.FromCtx(ctx)
	emitter, ok := l.Emitter().(*logrus.Emitter)
	if !ok {
		logger.Errorf(ctx, "the Emitter is not a *logrus.Emitter, but %T", l.Emitter())
		return ctx
	}
	l = l.WithHooks(NewHookAdapter(
		emitter.LogrusEntry.Logger,
		hook,
	))
	return logger.CtxWithLogger(ctx, l)
}
