package recoder

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func assert(
	ctx context.Context,
	mustBeTrue bool,
) {
	if mustBeTrue {
		return
	}

	logger.Panicf(ctx, "assertion failed")
}
