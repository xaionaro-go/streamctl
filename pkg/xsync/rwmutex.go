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
		logger.Panicf(ctx, "got a deadlock in:\n%s", allStacks)
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
		m.deadlockNotifier.Stop()
		m.cancelFunc()
		m.deadlockNotifier, m.cancelFunc = nil, nil

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

func DoA1[A0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0),
	a0 A0,
) {
	m.Do(ctx, func() {
		fn(a0)
	})
}

func DoA2[A0, A1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1),
	a0 A0,
	a1 A1,
) {
	m.Do(ctx, func() {
		fn(a0, a1)
	})
}

func DoA1R1[A0, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) R0,
	a0 A0,
) R0 {
	var (
		r0 R0
	)
	m.Do(ctx, func() {
		r0 = fn(a0)
	})
	return r0
}

func DoA2R1[A0, A1, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) R0,
	a0 A0,
	a1 A1,
) R0 {
	var (
		r0 R0
	)
	m.Do(ctx, func() {
		r0 = fn(a0, a1)
	})
	return r0
}

func DoA3R1[A0, A1, A2, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1, A2) R0,
	a0 A0,
	a1 A1,
	a2 A2,
) R0 {
	var (
		r0 R0
	)
	m.Do(ctx, func() {
		r0 = fn(a0, a1, a2)
	})
	return r0
}

func DoA4R1[A0, A1, A2, A3, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1, A2, A3) R0,
	a0 A0,
	a1 A1,
	a2 A2,
	a3 A3,
) R0 {
	var (
		r0 R0
	)
	m.Do(ctx, func() {
		r0 = fn(a0, a1, a2, a3)
	})
	return r0
}

func DoA1R2[A0, R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) (R0, R1),
	a0 A0,
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.Do(ctx, func() {
		r0, r1 = fn(a0)
	})
	return r0, r1
}

func DoA2R2[A0, A1, R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) (R0, R1),
	a0 A0,
	a1 A1,
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.Do(ctx, func() {
		r0, r1 = fn(a0, a1)
	})
	return r0, r1
}

func DoA2R3[A0, A1, R0, R1, R2 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) (R0, R1, R2),
	a0 A0,
	a1 A1,
) (R0, R1, R2) {
	var (
		r0 R0
		r1 R1
		r2 R2
	)
	m.Do(ctx, func() {
		r0, r1, r2 = fn(a0, a1)
	})
	return r0, r1, r2
}

func DoR1[R0 any](
	ctx context.Context,
	m *Mutex,
	fn func() R0,
) R0 {
	var (
		r0 R0
	)
	m.Do(ctx, func() {
		r0 = fn()
	})
	return r0
}

func DoR2[R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1),
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.Do(ctx, func() {
		r0, r1 = fn()
	})
	return r0, r1
}

func DoR3[R0, R1, R2 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1, R2),
) (R0, R1, R2) {
	var (
		r0 R0
		r1 R1
		r2 R2
	)
	m.Do(ctx, func() {
		r0, r1, r2 = fn()
	})
	return r0, r1, r2
}

func DoR4[R0, R1, R2, R3 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1, R2, R3),
) (R0, R1, R2, R3) {
	var (
		r0 R0
		r1 R1
		r2 R2
		r3 R3
	)
	m.Do(ctx, func() {
		r0, r1, r2, r3 = fn()
	})
	return r0, r1, r2, r3
}

func DoA1R4[A0, R0, R1, R2, R3 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) (R0, R1, R2, R3),
	a0 A0,
) (R0, R1, R2, R3) {
	var (
		r0 R0
		r1 R1
		r2 R2
		r3 R3
	)
	m.Do(ctx, func() {
		r0, r1, r2, r3 = fn(a0)
	})
	return r0, r1, r2, r3
}

func RDoA1[A0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0),
	a0 A0,
) {
	m.RDo(ctx, func() {
		fn(a0)
	})
}

func RDoA2[A0, A1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1),
	a0 A0,
	a1 A1,
) {
	m.RDo(ctx, func() {
		fn(a0, a1)
	})
}

func RDoA1R1[A0, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) R0,
	a0 A0,
) R0 {
	var (
		r0 R0
	)
	m.RDo(ctx, func() {
		r0 = fn(a0)
	})
	return r0
}

func RDoA2R1[A0, A1, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) R0,
	a0 A0,
	a1 A1,
) R0 {
	var (
		r0 R0
	)
	m.RDo(ctx, func() {
		r0 = fn(a0, a1)
	})
	return r0
}

func RDoA3R1[A0, A1, A2, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1, A2) R0,
	a0 A0,
	a1 A1,
	a2 A2,
) R0 {
	var (
		r0 R0
	)
	m.RDo(ctx, func() {
		r0 = fn(a0, a1, a2)
	})
	return r0
}

func RDoA4R1[A0, A1, A2, A3, R0 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1, A2, A3) R0,
	a0 A0,
	a1 A1,
	a2 A2,
	a3 A3,
) R0 {
	var (
		r0 R0
	)
	m.RDo(ctx, func() {
		r0 = fn(a0, a1, a2, a3)
	})
	return r0
}

func RDoA1R2[A0, R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) (R0, R1),
	a0 A0,
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.RDo(ctx, func() {
		r0, r1 = fn(a0)
	})
	return r0, r1
}

func RDoA2R2[A0, A1, R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) (R0, R1),
	a0 A0,
	a1 A1,
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.RDo(ctx, func() {
		r0, r1 = fn(a0, a1)
	})
	return r0, r1
}

func RDoA2R3[A0, A1, R0, R1, R2 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0, A1) (R0, R1, R2),
	a0 A0,
	a1 A1,
) (R0, R1, R2) {
	var (
		r0 R0
		r1 R1
		r2 R2
	)
	m.RDo(ctx, func() {
		r0, r1, r2 = fn(a0, a1)
	})
	return r0, r1, r2
}

func RDoR1[R0 any](
	ctx context.Context,
	m *Mutex,
	fn func() R0,
) R0 {
	var (
		r0 R0
	)
	m.RDo(ctx, func() {
		r0 = fn()
	})
	return r0
}

func RDoR2[R0, R1 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1),
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.RDo(ctx, func() {
		r0, r1 = fn()
	})
	return r0, r1
}

func RDoR3[R0, R1, R2 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1, R2),
) (R0, R1, R2) {
	var (
		r0 R0
		r1 R1
		r2 R2
	)
	m.RDo(ctx, func() {
		r0, r1, r2 = fn()
	})
	return r0, r1, r2
}

func RDoR4[R0, R1, R2, R3 any](
	ctx context.Context,
	m *Mutex,
	fn func() (R0, R1, R2, R3),
) (R0, R1, R2, R3) {
	var (
		r0 R0
		r1 R1
		r2 R2
		r3 R3
	)
	m.RDo(ctx, func() {
		r0, r1, r2, r3 = fn()
	})
	return r0, r1, r2, r3
}

func RDoA1R4[A0, R0, R1, R2, R3 any](
	ctx context.Context,
	m *Mutex,
	fn func(A0) (R0, R1, R2, R3),
	a0 A0,
) (R0, R1, R2, R3) {
	var (
		r0 R0
		r1 R1
		r2 R2
		r3 R3
	)
	m.RDo(ctx, func() {
		r0, r1, r2, r3 = fn(a0)
	})
	return r0, r1, r2, r3
}
