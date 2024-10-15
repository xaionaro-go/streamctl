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

type CtxKeyEnableDeadlock struct{}

func WithEnableDeadlock(ctx context.Context, enableDeadlock bool) context.Context {
	return context.WithValue(ctx, CtxKeyEnableDeadlock{}, enableDeadlock)
}

func IsEnableDeadlock(ctx context.Context) bool {
	v, ok := ctx.Value(CtxKeyNoLogging{}).(bool)
	if !ok {
		return true
	}
	return v
}
