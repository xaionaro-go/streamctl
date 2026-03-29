package youtube

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func (yt *YouTube) IsPlatformURL(u *url.URL) bool {
	return strings.Contains(u.Hostname(), "youtube")
}

func (yt *YouTube) IsChannelURL(u *url.URL) bool {
	h := u.Hostname()
	return strings.Contains(h, "youtube") || h == "youtu.be"
}

func (yt *YouTube) ExtractStreamID(u *url.URL) (streamcontrol.StreamID, error) {
	switch {
	case u.Hostname() == "youtu.be":
		id := strings.TrimPrefix(u.Path, "/")
		if id == "" {
			return "", fmt.Errorf("empty video ID in youtu.be URL: %s", u)
		}
		return streamcontrol.StreamID(id), nil
	case strings.HasPrefix(u.Path, "/watch"):
		v := u.Query().Get("v")
		if v == "" {
			return "", fmt.Errorf("missing 'v' query parameter in YouTube URL: %s", u)
		}
		return streamcontrol.StreamID(v), nil
	case strings.HasPrefix(u.Path, "/live/"):
		id := strings.TrimPrefix(u.Path, "/live/")
		if id == "" {
			return "", fmt.Errorf("empty video ID in YouTube live URL: %s", u)
		}
		return streamcontrol.StreamID(id), nil
	default:
		return "", fmt.Errorf("unsupported YouTube URL format: %s", u)
	}
}
