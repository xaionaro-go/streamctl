package streamserver

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/mediamtx/pkg/conf"
	mediamtxlogger "github.com/xaionaro-go/mediamtx/pkg/logger"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/mediamtx/pkg/servers/srt"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

type SRTServer struct {
	*srt.Server

	locker         xsync.Mutex
	originalConfig streamportserver.Config
	isInitialized  bool
}

func newSRTServer(
	pathManager *pathmanager.PathManager,
	listenAddr string,
	maxPayloadSize uint16,
	logger mediamtxlogger.Writer,
	opts ...streamportserver.Option,
) (*SRTServer, error) {
	psCfg := streamportserver.Options(opts).ProtocolSpecificConfig(context.Background())
	if psCfg.IsTLS {
		return nil, fmt.Errorf("TLS is not supported for SRT")
	}
	srv := &SRTServer{
		Server: &srt.Server{
			Address:             listenAddr,
			ReadTimeout:         conf.Duration(psCfg.ReadTimeout),
			WriteTimeout:        conf.Duration(psCfg.WriteTimeout),
			RunOnConnect:        "",
			RunOnConnectRestart: false,
			RunOnDisconnect:     "",
			ExternalCmdPool:     nil,
			PathManager:         pathManager,
			Parent:              logger,
			UDPMaxPayloadSize:   int(maxPayloadSize),
		},
		originalConfig: streamportserver.Config{
			ProtocolSpecificConfig: psCfg,
			Type:                   streamtypes.ServerTypeSRT,
			ListenAddr:             listenAddr,
		},
	}
	if err := srv.init(srv.originalConfig); err != nil {
		return nil, err
	}
	return srv, nil
}

func (srv *SRTServer) init(
	cfg streamportserver.Config,
) (_err error) {
	defer func() {
		if _err != nil {
			_ = srv.Close()
		}
	}()

	if err := srv.Initialize(); err != nil {
		return fmt.Errorf("Initialize() returned an error: %w", err)
	}
	srv.isInitialized = true

	return nil
}

var _ streamportserver.Server = (*SRTServer)(nil)

func (srv *SRTServer) ProtocolSpecificConfig() streamportserver.ProtocolSpecificConfig {
	return srv.originalConfig.ProtocolSpecificConfig
}

func (srv *SRTServer) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &srv.locker, func() error {
		if srv.Server == nil {
			return fmt.Errorf("already closed")
		}

		if srv.isInitialized {
			srv.Server.Close()
		}
		srv.Server = nil

		return nil
	})
}
func (srv *SRTServer) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeSRT
}
func (srv *SRTServer) ListenAddr() string {
	return srv.Server.Address
}
func (srv *SRTServer) NumBytesConsumerWrote() uint64 {
	result := uint64(0)
	list, err := srv.Server.APIConnsList()
	for _, item := range list.Items {
		result += item.BytesReceived
	}
	if err != nil {
		panic(err)
	}
	return result
}
func (srv *SRTServer) NumBytesProducerRead() uint64 {
	result := uint64(0)
	list, err := srv.Server.APIConnsList()
	for _, item := range list.Items {
		result += item.BytesSent
	}
	if err != nil {
		panic(err)
	}
	return result
}
