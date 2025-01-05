package recoder

import (
	"runtime"
	"sync"
)

type pool[T any] struct {
	sync.Pool
	ResetFunc func(*T)
}

func newPool[T any](
	allocFunc func() *T,
	resetFunc func(*T),
	freeFunc func(*T),
) *pool[T] {
	return &pool[T]{
		Pool: sync.Pool{
			New: func() any {
				v := allocFunc()
				runtime.SetFinalizer(v, func(v *T) {
					freeFunc(v)
				})
				return v
			},
		},
		ResetFunc: resetFunc,
	}
}

func (p *pool[T]) Get() *T {
	return p.Pool.Get().(*T)
}

func (p *pool[T]) Put(item *T) {
	p.ResetFunc(item)
	p.Pool.Put(item)
}
