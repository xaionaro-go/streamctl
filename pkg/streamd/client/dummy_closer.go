package client

import "io"

type dummyCloser struct{}

var _ io.Closer = dummyCloser{}

func (dummyCloser) Close() error {
	return nil
}
