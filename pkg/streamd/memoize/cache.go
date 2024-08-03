package memoize

import (
	"context"
)

type CtxKey string

const (
	CtxKeyNoCache = CtxKey("no_cache")
)

func IsNoCache(ctx context.Context) bool {
	v := ctx.Value(CtxKeyNoCache)
	if v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func SetNoCache(ctx context.Context, b bool) context.Context {
	return context.WithValue(ctx, CtxKeyNoCache, b)
}
