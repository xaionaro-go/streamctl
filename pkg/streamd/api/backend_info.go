package api

import (
	"reflect"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type BackendInfo struct {
	Data         any
	Capabilities map[streamcontrol.Capability]struct{}
}

var backendDataTypes = map[streamcontrol.PlatformID]reflect.Type{}

func RegisterBackendDataType(platID streamcontrol.PlatformID, t reflect.Type) {
	backendDataTypes[platID] = t
}

func NewBackendData(platID streamcontrol.PlatformID) any {
	t, ok := backendDataTypes[platID]
	if !ok {
		return nil
	}
	return reflect.New(t).Interface()
}
