package streamcontrol

import (
	"context"
	"fmt"
	"io"
	"time"
)

type AccountCommons interface {
	io.Closer
	fmt.Stringer

	GetPlatformID() PlatformID

	GetStreams(ctx context.Context) ([]StreamInfo, error)
	SetTitle(ctx context.Context, streamID StreamID, title string) error
	SetDescription(ctx context.Context, streamID StreamID, description string) error
	InsertAdsCuePoint(ctx context.Context, streamID StreamID, ts time.Time, duration time.Duration) error
	Flush(ctx context.Context, streamID StreamID) error
	SetStreamActive(ctx context.Context, streamID StreamID, isActive bool) error
	GetStreamStatus(ctx context.Context, streamID StreamID) (*StreamStatus, error)
	GetChatMessagesChan(ctx context.Context, streamID StreamID) (<-chan Event, error)
	SendChatMessage(ctx context.Context, streamID StreamID, message string) error
	RemoveChatMessage(ctx context.Context, streamID StreamID, messageID EventID) error
	BanUser(ctx context.Context, streamID StreamID, userID UserID, reason string, deadline time.Time) error
	Shoutout(ctx context.Context, streamID StreamID, chanID UserID) error
	RaidTo(ctx context.Context, streamID StreamID, chanID UserID) error

	CreateStream(ctx context.Context, title string) (StreamInfo, error)
	DeleteStream(ctx context.Context, streamID StreamID) error

	IsCapable(context.Context, Capability) bool
	IsChannelStreaming(ctx context.Context, chanID UserID) (bool, error)
}
