package goconv

import (
	"time"
)

func UnixGRPC2Go(unixNano int64) time.Time {
	return time.Unix(
		unixNano/int64(time.Second),
		unixNano%int64(time.Second),
	)
}
