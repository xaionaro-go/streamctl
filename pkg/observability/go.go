package observability

import (
	"context"
)

func Call(ctx context.Context, fn func()) {
	defer func() { PanicIfNotNil(ctx, recover()) }()
	fn()
}

func CallSafe(ctx context.Context, fn func()) {
	defer func() { ReportPanicIfNotNil(ctx, recover()) }()
	fn()
}

func Go(ctx context.Context, fn func()) {
	go Call(ctx, fn)
}

func GoSafe(ctx context.Context, fn func()) {
	go CallSafe(ctx, fn)
}
