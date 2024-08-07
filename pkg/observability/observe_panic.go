package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
)

func PanicIfNotNil(ctx context.Context, r any) {
	if r == nil {
		return
	}
	errmon.ObserveRecoverCtx(ctx, r)
	belt.Flush(ctx)
	time.Sleep(time.Second)
	panic(fmt.Sprintf("%#+v", r))
}
