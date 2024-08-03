package streamserver

import (
	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/yutopp-go-rtmp/streamserver"
)

type StreamPlayerStreamServer = streamserver.StreamPlayerStreamServer

func NewStreamPlayerStreamServer(s *StreamServer) *StreamPlayerStreamServer {
	return streamserver.NewStreamPlayerStreamServer(s)
}
