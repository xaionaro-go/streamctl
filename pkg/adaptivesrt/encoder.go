package adaptivesrt

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder"
)

type Encoder struct {
}

func NewEncoder(
	ctx context.Context,
	src string,
	dst string,
) (*Encoder, error) {
	proc, err := safeencoder.NewProcess(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize an encoder: %w", err)
	}

	proc.NewEncoder()
}
