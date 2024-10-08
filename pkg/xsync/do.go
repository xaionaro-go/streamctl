package xsync

import (
	"context"
)

type Doer interface {
	Do(context.Context, func())
}

func DoA1[A0 any](
	ctx context.Context,
	m Doer,
	fn func(A0),
	a0 A0,
) {
	m.Do(ctx, func() {
		fn(a0)
	})
}

func DoA2[A0, A1 any](
	ctx context.Context,
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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

func DoA3R2[A0, A1, A2, R0, R1 any](
	ctx context.Context,
	m Doer,
	fn func(A0, A1, A2) (R0, R1),
	a0 A0,
	a1 A1,
	a2 A2,
) (R0, R1) {
	var (
		r0 R0
		r1 R1
	)
	m.Do(ctx, func() {
		r0, r1 = fn(a0, a1, a2)
	})
	return r0, r1
}

func DoA4R1[A0, A1, A2, A3, R0 any](
	ctx context.Context,
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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
	m Doer,
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

type RDoer interface {
	RDo(context.Context, func())
}

func RDoA1[A0 any](
	ctx context.Context,
	m RDoer,
	fn func(A0),
	a0 A0,
) {
	m.RDo(ctx, func() {
		fn(a0)
	})
}

func RDoA2[A0, A1 any](
	ctx context.Context,
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
	m RDoer,
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
