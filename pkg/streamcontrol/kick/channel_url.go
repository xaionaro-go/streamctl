package kick

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (k *Kick) IsPlatformURL(u *url.URL) bool {
	return strings.Contains(u.Hostname(), "global-contribute.live-video.net")
}

func (k *Kick) IsChannelURL(u *url.URL) bool {
	h := u.Hostname()
	return h == "kick.com" || h == "www.kick.com"
}

func (k *Kick) ExtractStreamID(u *url.URL) (streamcontrol.StreamID, error) {
	channel := strings.TrimPrefix(u.Path, "/")
	channel = strings.TrimSuffix(channel, "/")
	if channel == "" {
		return "", fmt.Errorf("missing channel name in Kick URL: %s", u)
	}
	if idx := strings.Index(channel, "/"); idx != -1 {
		channel = channel[:idx]
	}
	return streamcontrol.StreamID(channel), nil
}
