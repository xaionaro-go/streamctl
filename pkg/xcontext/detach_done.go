package xcontext

import (
	"context"
	"time"
)

func DetachDone(ctx context.Context) context.Context {
	return ctxDetached{
		Context: ctx,
	}
}

type ctxDetached struct {
	context.Context
}

func (ctx ctxDetached) Deadline() (time.Time, bool) {
	return context.Background().Deadline()
}
func (ctx ctxDetached) Done() <-chan struct{} {
	return context.Background().Done()
}
func (ctx ctxDetached) Err() error {
	return context.Background().Err()
}
