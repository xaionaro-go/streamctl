package oto

import (
	"fmt"
	"io"
	"sync"
)

type repeatReaderT struct {
	io.Reader
	Locker     sync.Mutex
	ChunkSize  uint
	NumRepeats uint
	Buffer     []byte
}

func repeatReader(
	r io.Reader,
	chunkSize uint,
	nRepeats uint,
) *repeatReaderT {
	return &repeatReaderT{
		Reader:     r,
		ChunkSize:  chunkSize,
		NumRepeats: nRepeats,
	}
}

func (r *repeatReaderT) Read(p []byte) (int, error) {
	r.Locker.Lock()
	defer r.Locker.Unlock()
	chunksToRead := len(p) / int(r.ChunkSize) / int(r.NumRepeats)
	bytesToRead := chunksToRead * int(r.ChunkSize)
	if cap(r.Buffer) < bytesToRead {
		r.Buffer = make([]byte, bytesToRead)
	} else {
		r.Buffer = r.Buffer[:bytesToRead]
	}
	n, err := r.Reader.Read(r.Buffer)
	r.Buffer = r.Buffer[:n]

	if n%int(r.ChunkSize) != 0 {
		return 0, fmt.Errorf("read a number of bytes that is not a multiple of %d: %w", r.ChunkSize, err)
	}
	chunksToWrite := n / int(r.ChunkSize)

	for chunkIdx := 0; chunkIdx < chunksToWrite; chunkIdx++ {
		idxSrc := chunkIdx * int(r.ChunkSize)
		for repeatIdx := 0; repeatIdx < int(r.NumRepeats); repeatIdx++ {
			idxDst := (chunkIdx*int(r.NumRepeats) + repeatIdx) * int(r.ChunkSize)
			copy(p[idxDst:], r.Buffer[idxSrc:])
		}
	}
	return n * int(r.NumRepeats), err
}
