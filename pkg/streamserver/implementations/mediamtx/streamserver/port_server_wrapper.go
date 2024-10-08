package streamserver

import (
	"github.com/xaionaro-go/mediamtx/pkg/servers/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type portServerWrapperRTMP struct {
	*rtmp.Server
}

var _ types.PortServer = (*portServerWrapperRTMP)(nil)

func (w *portServerWrapperRTMP) Close() error {
	w.Server.Close()
	return nil
}
func (w *portServerWrapperRTMP) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeRTMP
}
func (w *portServerWrapperRTMP) ListenAddr() string {
	return w.Server.Address
}
func (w *portServerWrapperRTMP) NumBytesConsumerWrote() uint64 {
	result := uint64(0)
	list, err := w.Server.APIConnsList()
	for _, item := range list.Items {
		result += item.BytesReceived
	}
	if err != nil {
		panic(err)
	}
	return result
}
func (w *portServerWrapperRTMP) NumBytesProducerRead() uint64 {
	result := uint64(0)
	list, err := w.Server.APIConnsList()
	for _, item := range list.Items {
		result += item.BytesSent
	}
	if err != nil {
		panic(err)
	}
	return result
}
