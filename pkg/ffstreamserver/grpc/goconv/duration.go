package goconv

import "time"

func DurationToGRPC(d time.Duration) int64 {
	return d.Nanoseconds()
}

func DurationFromGRPC(d int64) time.Duration {
	return time.Nanosecond * time.Duration(d)
}
