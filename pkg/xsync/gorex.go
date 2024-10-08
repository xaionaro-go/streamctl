package xsync

import (
	"context"

	"github.com/xaionaro-go/gorex"
)

type Gorex struct {
	gorex.Mutex
}

func (g *Gorex) Do(ctx context.Context, callback func()) {
	g.Mutex.LockCtxDo(ctx, callback)
}
