package ringbuffer

import (
	"context"
	"slices"

	"github.com/xaionaro-go/xsync"
)

type RingBuffer[T comparable] struct {
	Storage           []T
	CurrentWriteIndex uint
	Locker            xsync.Mutex
}

func New[T comparable](size uint) *RingBuffer[T] {
	return &RingBuffer[T]{
		Storage: make([]T, 0, size),
	}
}

func (r *RingBuffer[T]) Add(item T) {
	r.Locker.Do(context.TODO(), func() {
		if r.CurrentWriteIndex >= uint(len(r.Storage)) {
			r.Storage = r.Storage[:len(r.Storage)+1]
		}
		r.Storage[r.CurrentWriteIndex] = item
		r.CurrentWriteIndex++
		if r.CurrentWriteIndex >= uint(cap(r.Storage)) {
			r.CurrentWriteIndex = 0
		}
	})
}

func (r *RingBuffer[T]) Contains(item T) bool {
	return xsync.DoR1(context.TODO(), &r.Locker, func() bool {
		return slices.Contains(r.Storage, item)
	})
}
