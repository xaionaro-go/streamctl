package xsync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
)

func fixCtx(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return ctx
}

type Mutex struct {
	mutex sync.Mutex

	cancelFunc       context.CancelFunc
	deadlockNotifier *time.Timer
}

func (m *Mutex) ManualLock(ctx context.Context) {
	ctx = fixCtx(ctx)
	noLogging := IsNoLogging(ctx)
	l := logger.FromCtx(ctx)
	if !noLogging {
		l.Tracef("locking")
	}
	m.mutex.Lock()

	ctx, m.cancelFunc = context.WithCancel(ctx)
	deadlockNotifier := time.NewTimer(time.Minute)
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-deadlockNotifier.C:
		}
		errmon.ObserveErrorCtx(ctx, fmt.Errorf("got a deadlock"))
	}()
	m.deadlockNotifier = deadlockNotifier

	if !noLogging {
		l.Tracef("locked")
	}
}

func (m *Mutex) ManualUnlock(ctx context.Context) {
	ctx = fixCtx(ctx)
	noLogging := IsNoLogging(ctx)
	l := logger.FromCtx(ctx)
	if !noLogging {
		l.Tracef("unlocking")
	}

	m.deadlockNotifier.Stop()
	m.cancelFunc()
	m.deadlockNotifier, m.cancelFunc = nil, nil

	m.mutex.Unlock()
	if !noLogging {
		l.Tracef("unlocked")
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
