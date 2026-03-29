package twitch

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (t *Twitch) IsPlatformURL(u *url.URL) bool {
	return strings.Contains(u.Hostname(), "twitch")
}

func (t *Twitch) IsChannelURL(u *url.URL) bool {
	return strings.Contains(u.Hostname(), "twitch")
}

func (t *Twitch) ExtractStreamID(u *url.URL) (streamcontrol.StreamID, error) {
	channel := strings.TrimPrefix(u.Path, "/")
	channel = strings.TrimSuffix(channel, "/")
	if channel == "" {
		return "", fmt.Errorf("missing channel name in Twitch URL: %s", u)
	}
	if idx := strings.Index(channel, "/"); idx != -1 {
		channel = channel[:idx]
	}
	return streamcontrol.StreamID(channel), nil
}
