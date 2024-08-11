package observability

import (
	"context"
)

func Go(ctx context.Context, fn func()) {
	go func() {
		defer func() { PanicIfNotNil(ctx, recover()) }()
		fn()
	}()
}

func GoSafe(ctx context.Context, fn func()) {
	go func() {
		defer func() { ReportPanicIfNotNil(ctx, recover()) }()
		fn()
	}()
}
