package buildvars

import (
	"strconv"
	"time"
)

var (
	GitCommit       string
	Version         string
	BuildDateString string
	BuildDate       *time.Time

	TwitchClientID     string
	TwitchClientSecret string
)

func init() {
	unixTS, err := strconv.ParseInt(BuildDateString, 10, 64)
	if err == nil {
		BuildDate = ptr(time.Unix(unixTS, 0))
	}
}
