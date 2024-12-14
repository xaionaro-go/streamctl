package whisper

import "io"

type noopCloser struct {
	io.ReadWriter
}

func (noopCloser) Close() error {
	return nil
}
