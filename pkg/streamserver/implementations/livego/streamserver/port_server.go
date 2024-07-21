package streamserver

import (
	"net"
	"sync/atomic"

	"github.com/gwuhaolin/livego/protocol/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
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
func (srv *PortServer) Type() types.ServerType {
	return types.ServerTypeRTMP
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
