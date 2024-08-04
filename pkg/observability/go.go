package observability

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
)

func Go(ctx context.Context, fn func()) {
	go func() {
		defer func() { PanicIfNotNil(ctx, recover()) }()
		fn()
	}()
}

func GoSafe(ctx context.Context, fn func()) {
	go func() {
		defer func() { errmon.ObserveRecoverCtx(ctx, recover()) }()
		fn()
	}()
}
