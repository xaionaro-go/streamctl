package encoder

import (
	"context"
	"io"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type Encoder interface {
	io.Closer

	NewInputFromURL(context.Context, string, string, InputConfig) (Input, error)
	NewOutputFromURL(context.Context, string, string, OutputConfig) (Output, error)
	StartRecoding(context.Context, Input, Output) error
	WaitForRecordingEnd(context.Context) error
	GetStats(context.Context) (*Stats, error)
}

type NewInputFromPublisherer interface {
	NewInputFromPublisher(context.Context, types.Publisher, InputConfig) (Input, error)
}

type Factory interface {
	New(context.Context, Config) (Encoder, error)
}
