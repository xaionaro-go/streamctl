package youtube

import "google.golang.org/api/youtube/v3"

type StreamStatusCustomData struct {
	ActiveBroadcasts   []*youtube.LiveBroadcast
	UpcomingBroadcasts []*youtube.LiveBroadcast
	Streams            []*youtube.LiveStream
}
