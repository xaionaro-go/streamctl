package api

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type BackendInfo struct {
	Data         any
	Capabilities map[streamcontrol.Capability]struct{}
}
