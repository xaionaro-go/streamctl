package loggedsync

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type Mutex struct {
	xsync.Mutex
}

func (m *Mutex) LockCtx(ctx context.Context) {
	logger.Tracef(ctx, "")
	m.Mutex.Lock()
}
