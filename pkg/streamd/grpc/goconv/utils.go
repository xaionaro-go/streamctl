package goconv

import (
	"time"
)

func secsGRPC2Go(secs float64) time.Duration {
	return time.Duration(float64(time.Second) * secs)
}
