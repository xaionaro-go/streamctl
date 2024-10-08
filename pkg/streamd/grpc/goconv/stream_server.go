package goconv

import (
	"context"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func StreamServerTypeGo2GRPC(t api.StreamServerType) (streamd_grpc.StreamServerType, error) {
	switch t {
	case streamtypes.ServerTypeUndefined:
		return streamd_grpc.StreamServerType_Undefined, nil
	case streamtypes.ServerTypeRTMP:
		return streamd_grpc.StreamServerType_RTMP, nil
	case streamtypes.ServerTypeRTSP:
		return streamd_grpc.StreamServerType_RTSP, nil
	}
	return streamd_grpc.StreamServerType_Undefined, fmt.Errorf("unexpected value: %v", t)
}

func StreamServerTypeGRPC2Go(t streamd_grpc.StreamServerType) (api.StreamServerType, error) {
	switch t {
	case streamd_grpc.StreamServerType_Undefined:
		return streamtypes.ServerTypeUndefined, nil
	case streamd_grpc.StreamServerType_RTMP:
		return streamtypes.ServerTypeRTMP, nil
	case streamd_grpc.StreamServerType_RTSP:
		return streamtypes.ServerTypeRTSP, nil
	}
	return streamtypes.ServerTypeUndefined, fmt.Errorf("unexpected value: %v", t)
}

func StreamServerConfigGo2GRPC(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
	opts ...types.ServerOption,
) (*streamd_grpc.StreamServer, error) {
	t, err := StreamServerTypeGo2GRPC(serverType)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the server type: %w", err)
	}

	cfg := types.ServerOptions(opts).Config(ctx)
	var serverCert *streamd_grpc.TLSCertificate
	if cfg.ServerCert != nil {
		serverCert = &streamd_grpc.TLSCertificate{
			TLSCertificateOneOf: &streamd_grpc.TLSCertificate_X509{
				X509: cfg.ServerCert.Raw,
			},
		}
	}

	var privateKey *streamd_grpc.PrivateKey
	if cfg.ServerKey != nil {
		b, err := x509.MarshalPKCS8PrivateKey(cfg.ServerKey)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize the private key to PKCS8: %w", err)
		}
		privateKey = &streamd_grpc.PrivateKey{
			PrivateKeyOneOf: &streamd_grpc.PrivateKey_PKCS8{
				PKCS8: b,
			},
		}
	}

	return &streamd_grpc.StreamServer{
		ServerType:       t,
		ListenAddr:       listenAddr,
		IsTLS:            cfg.IsTLS,
		WriteQueueSize:   cfg.WriteQueueSize,
		WriteTimeoutNano: uint64(cfg.WriteTimeout.Nanoseconds()),
		ReadTimeoutNano:  uint64(cfg.ReadTimeout.Nanoseconds()),
		ServerCert:       serverCert,
		ServerKey:        privateKey,
	}, nil
}

func StreamServerConfigGRPC2Go(
	ctx context.Context,
	srv *streamd_grpc.StreamServer,
) (api.StreamServerType, string, types.ServerOptions, error) {
	srvType, err := StreamServerTypeGRPC2Go(srv.GetServerType())
	if err != nil {
		return 0, "", nil, fmt.Errorf("unable to convert the server type value: %w", err)
	}

	serverCert, err := TLSCertificateGRPC2Go(srv.GetServerCert())
	if err != nil {
		return 0, "", nil, fmt.Errorf("unable to convert the TLS certificate: %w", err)
	}

	serverKey, err := PrivateKeyGRPC2Go(srv.GetServerKey())
	if err != nil {
		return 0, "", nil, fmt.Errorf("unable to convert the private key: %w", err)
	}

	cfg := &types.ServerConfig{
		IsTLS:          srv.GetIsTLS(),
		WriteQueueSize: srv.GetWriteQueueSize(),
		WriteTimeout:   time.Nanosecond * time.Duration(srv.GetWriteTimeoutNano()),
		ReadTimeout:    time.Nanosecond * time.Duration(srv.GetReadTimeoutNano()),
		ServerCert:     serverCert,
		ServerKey:      serverKey,
	}

	return srvType, srv.GetListenAddr(), cfg.Options(), nil
}
