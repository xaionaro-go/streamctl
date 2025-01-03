package livego

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type Output struct {
	URL string
}

func (r *Encoder) NewOutputFromURL(
	ctx context.Context,
	url string,
	cfg recoder.OutputConfig,
) (recoder.Output, error) {
	return &Output{
		URL: url,
	}, nil
}

func (r *Output) Close() error {
	return nil
}
