package streamserver

import (
	"context"

	"github.com/xaionaro-go/mediamtx/pkg/conf"
	"github.com/xaionaro-go/mediamtx/pkg/logger"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/mediamtx/pkg/servers/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

func newRTMPServer(
	pathManager *pathmanager.PathManager,
	listenAddr string,
	logger logger.Writer,
	opts ...types.ServerOption,
) *rtmp.Server {
	cfg := types.ServerOptions(opts).Config(context.Background())
	//s.BackendServer.Initialize()
	if cfg.ServerCert != nil {
		panic("not implemented, yet")
	}
	if cfg.ServerKey != nil {
		panic("not implemented, yet")
	}
	return &rtmp.Server{
		Address:             listenAddr,
		ReadTimeout:         conf.StringDuration(cfg.ReadTimeout),
		WriteTimeout:        conf.StringDuration(cfg.WriteTimeout),
		IsTLS:               cfg.IsTLS,
		ServerCert:          "",
		ServerKey:           "",
		RTSPAddress:         "",
		RunOnConnect:        "",
		RunOnConnectRestart: false,
		RunOnDisconnect:     "",
		ExternalCmdPool:     nil,
		PathManager:         pathManager,
		Parent:              logger,
	}
}
