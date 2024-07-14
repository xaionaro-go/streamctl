package loggedsync

import (
	"context"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type Mutex struct {
	sync.Mutex
}

func (m *Mutex) LockCtx(ctx context.Context) {
	logger.Tracef(ctx, "")
	m.Mutex.Lock()
}
