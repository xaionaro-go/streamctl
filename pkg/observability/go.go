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
