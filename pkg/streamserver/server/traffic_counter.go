package server

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

type NumBytesWroter interface {
	NumBytesWrote() uint64
}

type NumBytesReader interface {
	NumBytesRead() uint64
}

type NumBytesReaderWroter interface {
	NumBytesReader
	NumBytesWroter
}

type Counter interface {
	Count() uint64
}

type TrafficCounter struct {
	sync.Mutex
	ReaderCounter Counter
	WriterCounter Counter
}

func (tc *TrafficCounter) NumBytesWrote() uint64 {
	if tc == nil {
		return math.MaxUint64
	}

	tc.Lock()
	defer tc.Unlock()
	if tc.WriterCounter == nil {
		return math.MaxUint64
	}
	return tc.WriterCounter.Count()
}

func (tc *TrafficCounter) NumBytesRead() uint64 {
	if tc == nil {
		return math.MaxUint64
	}

	tc.Lock()
	defer tc.Unlock()
	if tc.ReaderCounter == nil {
		return math.MaxUint64
	}
	fmt.Println(tc.ReaderCounter.Count())
	return tc.ReaderCounter.Count()
}

type IntPtrCounter struct {
	Pointer *int
}

func NewIntPtrCounter(v *int) *IntPtrCounter {
	return &IntPtrCounter{
		Pointer: v,
	}
}

func (c *IntPtrCounter) Count() uint64 {
	if c == nil || c.Pointer == nil {
		return math.MaxUint64
	}

	return uint64(atomic.LoadInt32((*int32)(unsafe.Pointer(c.Pointer))))
}
