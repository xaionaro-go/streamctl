package audio

import (
	"io"
	"unsafe"
)

type float32Reader interface {
	Read([]float32) (int, error)
}

type readerFromFloat32Reader struct {
	float32Reader
}

var _ io.Reader = (*readerFromFloat32Reader)(nil)

func newReaderFromFloat32Reader(r float32Reader) readerFromFloat32Reader {
	return readerFromFloat32Reader{
		float32Reader: r,
	}
}

func (r readerFromFloat32Reader) Read(b []byte) (int, error) {
	ptr := unsafe.SliceData(b)
	f := unsafe.Slice((*float32)(unsafe.Pointer(ptr)), len(b)/4)
	n, err := r.float32Reader.Read(f)
	n *= int(unsafe.Sizeof(float32(0)))
	return n, err
}
