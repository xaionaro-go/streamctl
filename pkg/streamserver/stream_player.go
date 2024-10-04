package streamserver

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamforward"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type StreamPlayerStreamServer[AF any] struct {
	types.StreamServer[AF]
}

var _ streamplayer.StreamServer = (*StreamPlayerStreamServer[any])(nil)

type ActiveStreamForwarding = *streamforward.ActiveStreamForwarding

func NewStreamPlayerStreamServer(
	ss types.StreamServer[ActiveStreamForwarding],
) *StreamPlayerStreamServer[ActiveStreamForwarding] {
	return CustomNewStreamPlayerStreamServer(ss)
}

func CustomNewStreamPlayerStreamServer[AF any](ss types.StreamServer[AF]) *StreamPlayerStreamServer[AF] {
	return &StreamPlayerStreamServer[AF]{
		StreamServer: ss,
	}
}

func (s *StreamPlayerStreamServer[AF]) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	srvs := s.StreamServer.ListServers(ctx)

	result := make([]streamplayer.StreamPortServer, 0, len(srvs))
	for _, srv := range srvs {
		result = append(result, streamplayer.StreamPortServer{
			Addr: srv.ListenAddr(),
			Type: srv.Type(),
		})
	}

	return result, nil
}
