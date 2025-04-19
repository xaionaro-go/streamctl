package streamserver

import (
	"context"
	"fmt"
	"net"

	"github.com/xaionaro-go/mediamtx/pkg/servers/hls"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
)

func newHLSServer() *hls.Server {
	panic("not implemented")
	return &hls.Server{
		Address:         "",
		Encryption:      false,
		ServerKey:       "",
		ServerCert:      "",
		AllowOrigin:     "",
		TrustedProxies:  []net.IPNet{},
		AlwaysRemux:     false,
		Variant:         0,
		SegmentCount:    0,
		SegmentDuration: 0,
		PartDuration:    0,
		SegmentMaxSize:  0,
		Directory:       "",
		ReadTimeout:     0,
		MuxerCloseAfter: 0,
		PathManager:     nil,
		Parent:          nil,
	}
}

func (s *StreamServer) newServerHLS(
	ctx context.Context,
	listenAddr string,
	opts ...streamportserver.Option,
) (_ streamportserver.Server, _ret error) {
	return nil, fmt.Errorf("support of HLS is not implemented, yet")
}

func (s *StreamServer) newServerWebRTC(
	ctx context.Context,
	listenAddr string,
	opts ...streamportserver.Option,
) (_ streamportserver.Server, _ret error) {
	return nil, fmt.Errorf("support of WebRTC is not implemented, yet")
}
