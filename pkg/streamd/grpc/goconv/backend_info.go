package goconv

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func BackendDataGRPC2Go(
	platID streamcontrol.PlatformID,
	dataString string,
) (any, error) {
	data := api.NewBackendData(platID)
	if data == nil {
		return nil, fmt.Errorf(
			"unknown platform: '%s'",
			platID,
		)
	}
	err := json.Unmarshal([]byte(dataString), data)
	if err != nil {
		return nil, fmt.Errorf("unable to unmarshal backend data for platform %s: %w", platID, err)
	}
	return reflect.ValueOf(data).Elem().Interface(), nil
}
