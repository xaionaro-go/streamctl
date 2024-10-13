package recoder

import (
	"context"
	"io"
)

type Stats struct {
	BytesCountRead  uint64
	BytesCountWrote uint64
}

type Recoder interface {
	io.Closer

	NewInputFromURL(context.Context, string, InputConfig) (Input, error)
	NewOutputFromURL(context.Context, string, OutputConfig) (Output, error)
	StartRecoding(context.Context, Input, Output) error
	WaitForRecordingEnd(context.Context) error
	GetStats(context.Context) (*Stats, error)
}

type Factory interface {
	New(context.Context, Config) (Recoder, error)
}
