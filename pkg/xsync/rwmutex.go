package xsync

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type lockType uint

const (
	lockTypeUndefined = lockType(iota)
	lockTypeWrite
	lockTypeRead
)

func (lt lockType) String() string {
	switch lt {
	case lockTypeUndefined:
		return "<undefined>"
	case lockTypeWrite:
		return "Lock"
	case lockTypeRead:
		return "RLock"
	default:
		return fmt.Sprintf("<unexpected_value_%d>", uint(lt))
	}
}

func fixCtx(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx
}

type Mutex = RWMutex

type RWMutex struct {
	mutex sync.RWMutex

	OverrideTimeout  time.Duration
	PanicOnDeadlock  *bool
	cancelFunc       context.CancelFunc
	deadlockNotifier *time.Timer
}

func (m *Mutex) ManualRLock(ctx context.Context) {
	m.manualLock(ctx, lockTypeRead)
}

func (m *Mutex) ManualLock(ctx context.Context) {
	m.manualLock(ctx, lockTypeWrite)
}

func (m *Mutex) manualLock(ctx context.Context, lockType lockType) {
	ctx = fixCtx(ctx)
	noLogging := IsNoLogging(ctx)
	l := logger.FromCtx(ctx)
	if !noLogging {
		l.Tracef("%sing", lockType)
	}
	switch lockType {
	case lockTypeWrite:
		m.mutex.Lock()
		m.startDeadlockDetector(ctx)
	case lockTypeRead:
		m.mutex.RLock()
	}

	if !noLogging {
		l.Tracef("%sed", lockType)
	}
}

func (m *Mutex) startDeadlockDetector(ctx context.Context) {
	ctx, m.cancelFunc = context.WithCancel(ctx)
	timeout := time.Minute
	if m.OverrideTimeout != 0 {
		timeout = m.OverrideTimeout
	}
	if timeout <= 0 {
		return
	}

	deadlockNotifier := time.NewTimer(time.Minute)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-deadlockNotifier.C:
		}
		allStacks := make([]byte, 1024*1024)
		n := runtime.Stack(allStacks, true)
		allStacks = allStacks[:n]
		if IsEnableDeadlock(ctx) {
			if m.PanicOnDeadlock == nil || *m.PanicOnDeadlock {
				logger.Panicf(ctx, "got a deadlock in:\n%s", allStacks)
			} else {
				logger.Errorf(ctx, "got a deadlock in:\n%s", allStacks)
			}
		}
	}()
	m.deadlockNotifier = deadlockNotifier
}

func (m *Mutex) ManualTryRLock(ctx context.Context) bool {
	return m.manualTryLock(ctx, lockTypeRead)
}

func (m *Mutex) ManualTryLock(ctx context.Context) bool {
	return m.manualTryLock(ctx, lockTypeWrite)
}

func (m *Mutex) manualTryLock(ctx context.Context, lockType lockType) bool {
	ctx = fixCtx(ctx)
	noLogging := IsNoLogging(ctx)
	l := logger.FromCtx(ctx)
	if !noLogging {
		l.Tracef("Try%sing", lockType)
	}

	var result bool
	switch lockType {
	case lockTypeWrite:
		result = m.mutex.TryLock()
		if result {
			m.startDeadlockDetector(ctx)
		}
	case lockTypeRead:
		result = m.mutex.TryRLock()
	}

	if !noLogging {
		l.Tracef("Try%sed, result: %v", lockType, result)
	}
	return result
}

func (m *Mutex) ManualRUnlock(ctx context.Context) {
	m.manualUnlock(ctx, lockTypeRead)
}

func (m *Mutex) ManualUnlock(ctx context.Context) {
	m.manualUnlock(ctx, lockTypeWrite)
}

func (m *Mutex) manualUnlock(ctx context.Context, lockType lockType) {
	ctx = fixCtx(ctx)
	noLogging := IsNoLogging(ctx)
	l := logger.FromCtx(ctx)
	if !noLogging {
		l.Tracef("un%sing", lockType)
	}

	switch lockType {
	case lockTypeWrite:
		if m.deadlockNotifier != nil {
			m.deadlockNotifier.Stop()
			m.cancelFunc()
			m.deadlockNotifier, m.cancelFunc = nil, nil
		}

		m.mutex.Unlock()
	case lockTypeRead:
		m.mutex.RUnlock()
	}

	if !noLogging {
		l.Tracef("un%sed", lockType)
	}
}

func (m *Mutex) Do(
	ctx context.Context,
	fn func(),
) {
	m.ManualLock(ctx)
	defer m.ManualUnlock(ctx)
	fn()
}

func (m *Mutex) RDo(
	ctx context.Context,
	fn func(),
) {
	m.ManualRLock(ctx)
	defer m.ManualRUnlock(ctx)
	fn()
}

func (m *Mutex) UDo(
	ctx context.Context,
	fn func(),
) {
	m.ManualUnlock(ctx)
	defer m.ManualLock(ctx)
	fn()
}

func (m *Mutex) URDo(
	ctx context.Context,
	fn func(),
) {
	m.ManualRUnlock(ctx)
	defer m.ManualRLock(ctx)
	fn()
}
