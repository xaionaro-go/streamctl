package streams

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func (s *Stream) Publish(
	ctx context.Context,
	url string,
) (*StreamForwarding, error) {
	streamFwd := NewStreamForwarding(s.streamHandler)
	err := streamFwd.Start(ctx, s, url)
	if err != nil {
		return nil, fmt.Errorf("unable to start stream forwarding to '%s': %w", url, err)
	}

	s.mu.Do(ctx, func() {
		s.cleanup()
		s.forwardings = append(s.forwardings, streamFwd)
	})
	return streamFwd, nil
}

func (s *Stream) Cleanup() {
	ctx := context.TODO()
	s.mu.Do(ctx, func() {
		s.cleanup()
	})
}

func (s *Stream) cleanup() {
	c := make([]*StreamForwarding, 0, len(s.forwardings))
	for _, fwd := range s.forwardings {
		if fwd.IsClosed() {
			continue
		}
		c = append(c, fwd)
	}
	s.forwardings = c
}

func (s *Stream) Forwardings() []*StreamForwarding {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &s.mu, func() []*StreamForwarding {
		s.cleanup()
		c := make([]*StreamForwarding, 0, len(s.forwardings))
		c = append(c, s.forwardings...)
		return c
	})
}

func (s *StreamHandler) Publish(
	ctx context.Context,
	stream *Stream,
	destination any,
) {
	switch v := destination.(type) {
	case string:
		if _, err := stream.Publish(ctx, v); err != nil {
			logger.Default().Error(err)
		}
	case []any:
		for _, v := range v {
			s.Publish(ctx, stream, v)
		}
	}
}
