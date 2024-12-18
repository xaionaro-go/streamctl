package livego

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
)

type Input struct {
	URL string
}

var _ recoder.Input = (*Input)(nil)

func (r *Encoder) NewInputFromURL(
	ctx context.Context,
	url string,
	cfg recoder.InputConfig,
) (recoder.Input, error) {
	return &Input{
		URL: url,
	}, nil
}

func (r *Input) Close() error {
	return nil
}
