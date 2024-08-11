package types

import (
	"fmt"
	"io"
	"math"
	"sync/atomic"
	"unsafe"

	"github.com/sasha-s/go-deadlock"
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
	deadlock.Mutex
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

type Uint64PtrCounter struct {
	Pointer *uint64
}

func (c *Uint64PtrCounter) Add(a uint64) uint64 {
	if c == nil || c.Pointer == nil {
		return math.MaxUint64
	}

	return atomic.AddUint64(c.Pointer, a)
}

func (c *Uint64PtrCounter) Count() uint64 {
	if c == nil || c.Pointer == nil {
		return math.MaxUint64
	}

	return atomic.LoadUint64(c.Pointer)
}

type ReaderWriteCloseCounter struct {
	Backend      io.ReadWriteCloser
	ReadCounter  Uint64PtrCounter
	WriteCounter Uint64PtrCounter
}

func NewReaderWriterCloseCounter(
	backend io.ReadWriteCloser,
	readCountPtr, writeCountPtr *uint64,
) *ReaderWriteCloseCounter {
	return &ReaderWriteCloseCounter{
		Backend: backend,
		ReadCounter: Uint64PtrCounter{
			Pointer: readCountPtr,
		},
		WriteCounter: Uint64PtrCounter{
			Pointer: writeCountPtr,
		},
	}
}

func (h *ReaderWriteCloseCounter) Write(b []byte) (int, error) {
	n, err := h.Backend.Write(b)
	if n > 0 {
		h.WriteCounter.Add(uint64(len(b)))
	}
	return n, err
}

func (h *ReaderWriteCloseCounter) Read(b []byte) (int, error) {
	n, err := h.Backend.Read(b)
	if n > 0 {
		h.ReadCounter.Add(uint64(len(b)))
	}
	return n, err
}

func (h *ReaderWriteCloseCounter) Close() error {
	return h.Backend.Close()
}
