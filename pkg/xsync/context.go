package xsync

import (
	"context"
)

type CtxKeyNoLogging struct{}

func WithNoLogging(ctx context.Context, noLogging bool) context.Context {
	return context.WithValue(ctx, CtxKeyNoLogging{}, noLogging)
}

func IsNoLogging(ctx context.Context) bool {
	v, _ := ctx.Value(CtxKeyNoLogging{}).(bool)
	return v
}
