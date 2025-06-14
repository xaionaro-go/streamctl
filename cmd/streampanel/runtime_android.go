package main

import (
	"time"

	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/xsync"
)

func init() {
	xpath.HomeDirOverride = "/data/user/0/center.dx.streampanel/files"
	xsync.DefaultDeadlockTimeout = time.Hour
}
