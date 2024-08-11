package loggedsync

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/sasha-s/go-deadlock"
)

type Mutex struct {
	deadlock.Mutex
}

func (m *Mutex) LockCtx(ctx context.Context) {
	logger.Tracef(ctx, "")
	m.Mutex.Lock()
}
