package streamserver

import (
	"net"
	"sync/atomic"

	"github.com/gwuhaolin/livego/protocol/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type PortServer struct {
	Stream     *rtmp.RtmpStream
	Server     *rtmp.Server
	Listener   net.Listener
	ReadCount  uint64
	WriteCount uint64
}

var _ types.PortServer = (*PortServer)(nil)

func (srv *PortServer) Close() error {
	return srv.Listener.Close()
}
func (srv *PortServer) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeRTMP
}
func (srv *PortServer) ListenAddr() string {
	return srv.Listener.Addr().String()
}
func (srv *PortServer) NumBytesConsumerWrote() uint64 {
	return atomic.LoadUint64(&srv.WriteCount)
}
func (srv *PortServer) NumBytesProducerRead() uint64 {
	return atomic.LoadUint64(&srv.ReadCount)
}
