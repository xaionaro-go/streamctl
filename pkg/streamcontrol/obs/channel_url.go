package obs

import (
	"fmt"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (o *OBS) IsPlatformURL(*url.URL) bool {
	return false
}

func (o *OBS) IsChannelURL(*url.URL) bool {
	return false
}

func (o *OBS) ExtractStreamID(u *url.URL) (streamcontrol.StreamID, error) {
	return "", fmt.Errorf("OBS does not support channel URLs")
}
