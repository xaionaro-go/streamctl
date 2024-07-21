package streamserver

import (
	"context"
	"fmt"

	"github.com/gwuhaolin/livego/protocol/rtmp/rtmprelay"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type ActiveStreamForwarding struct {
	StreamID      types.StreamID
	DestinationID types.DestinationID
	RtmpRelay     *rtmprelay.RtmpRelay
}

func newActiveStreamForward(
	_ context.Context,
	streamID types.StreamID,
	dstID types.DestinationID,
	urlSrc string,
	urlDst string,
) (*ActiveStreamForwarding, error) {
	relay := rtmprelay.NewRtmpRelay(&urlSrc, &urlDst)
	if err := relay.Start(); err != nil {
		return nil, fmt.Errorf("unable to start RTMP relay from '%s' to '%s': %w", urlSrc, urlDst, err)
	}
	return &ActiveStreamForwarding{
		StreamID:      streamID,
		DestinationID: dstID,
		RtmpRelay:     relay,
	}, nil
}

func (fwd *ActiveStreamForwarding) Close() error {
	fwd.RtmpRelay.Stop()
	return nil
}
