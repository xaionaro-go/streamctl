package goconv

import (
	"encoding/json"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
)

func BackendDataGRPC2Go(
	platID streamcontrol.PlatformName,
	dataString string,
) (any, error) {
	var data any
	var err error
	switch platID {
	case obs.ID:
		_data := api.BackendDataOBS{}
		err = json.Unmarshal([]byte(dataString), &_data)
		data = _data
	case twitch.ID:
		_data := api.BackendDataTwitch{}
		err = json.Unmarshal([]byte(dataString), &_data)
		data = _data
	case kick.ID:
		_data := api.BackendDataKick{}
		err = json.Unmarshal([]byte(dataString), &_data)
		data = _data
	case youtube.ID:
		_data := api.BackendDataYouTube{}
		err = json.Unmarshal([]byte(dataString), &_data)
		data = _data
	default:
		return nil, fmt.Errorf(
			"unknown platform: '%s'",
			platID,
		)
	}
	return data, err
}
