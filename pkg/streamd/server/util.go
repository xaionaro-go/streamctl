package server

import (
	"time"
)

func ptr[T any](in T) *T {
	return &in
}

func sec2dur(in float64) time.Duration {
	return time.Duration(float64(time.Second) * in)
}
