package youtube

import (
	"context"
	"fmt"
)

func checkCtx(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("the context is done: %w", ctx.Err())
	default:
		return nil
	}
}
