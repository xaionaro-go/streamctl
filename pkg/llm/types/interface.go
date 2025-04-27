package types

import (
	"context"
	"io"
)

type LLM interface {
	io.Closer
	Generate(ctx context.Context, prompt string) (string, error)
}
