package streampanel

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func must[T any](in T, err error) T {
	if err != nil {
		panic(err)
	}
	return in
}

func ignoreError[T any](in T, err error) T {
	if err != nil {
		logger.Errorf(context.TODO(), "error %v was ignored", err)
	}
	return in
}
