package xsync

import (
	"sync"
)

type Map[K comparable, V any] struct {
	sync.Map
}

func (m *Map[K, V]) CompareAndDelete(key K, old V) (deleted bool) {
	return m.Map.CompareAndDelete(key, old)
}

func (m *Map[K, V]) CompareAndSwap(key K, old, new V) (swapped bool) {
	return m.Map.CompareAndSwap(key, old, new)
}

func (m *Map[K, V]) Delete(key K) {
	m.Map.Delete(key)
}

func (m *Map[K, V]) Load(key K) (V, bool) {
	value, ok := m.Map.Load(key)
	if !ok {
		var zeroValue V
		return zeroValue, false
	}
	return value.(V), true
}

func (m *Map[K, V]) LoadAndDelete(key K) (V, bool) {
	value, loaded := m.Map.LoadAndDelete(key)
	if !loaded {
		var zeroValue V
		return zeroValue, false
	}
	return value.(V), true
}
func (m *Map[K, V]) LoadOrStore(key K, value V) (V, bool) {
	actual, loaded := m.Map.LoadOrStore(key, value)
	if !loaded {
		var zeroValue V
		return zeroValue, false
	}
	return actual.(V), true
}

func (m *Map[K, V]) Range(f func(key K, value V) bool) {
	m.Map.Range(func(key, value any) bool {
		return f(key.(K), value.(V))
	})
}

func (m *Map[K, V]) Store(key K, value V) {
	m.Map.Store(key, value)
}

func (m *Map[K, V]) Swap(key K, value V) (V, bool) {
	previous, loaded := m.Map.Swap(key, value)
	if !loaded {
		var zeroValue V
		return zeroValue, false
	}
	return previous.(V), true
}
