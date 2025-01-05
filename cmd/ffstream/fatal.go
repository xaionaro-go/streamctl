package main

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func assertNoError(
	ctx context.Context,
	err error,
) {
	if err != nil {
		logger.Fatal(ctx, err)
	}
}

func fatal(
	ctx context.Context,
	format string,
	args ...any,
) {
	logger.Fatalf(ctx, format, args...)
}
