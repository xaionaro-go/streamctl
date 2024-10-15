package streamserver

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"fmt"
	"io"
	"os"

	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	"github.com/xaionaro-go/mediamtx/pkg/logger"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/mediamtx/pkg/servers/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type RTMPServer struct {
	Locker xsync.Mutex
	*rtmp.Server
}

func newRTMPServer(
	pathManager *pathmanager.PathManager,
	listenAddr string,
	logger logger.Writer,
	opts ...types.ServerOption,
) (*RTMPServer, error) {
	cfg := types.ServerOptions(opts).Config(context.Background())
	srv := &RTMPServer{Server: &rtmp.Server{
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
	}}
	if err := srv.init(cfg); err != nil {
		return nil, err
	}
	return srv, nil
}

func (srv *RTMPServer) init(
	cfg types.ServerConfig,
) error {
	if (cfg.ServerCert != nil) != (cfg.ServerKey != nil) {
		return fmt.Errorf("fields 'ServerCert' and 'ServerKey' should be used together (cur values: ServerCert == %#+v; ServerKey == %#+v)", cfg.ServerCert, cfg.ServerKey)
	}

	if cfg.ServerCert != nil && cfg.ServerKey != nil {
		err := srv.setServerCertificate(*cfg.ServerCert, cfg.ServerKey)
		if err != nil {
			return fmt.Errorf("unable to set the TLS certificate: %w", err)
		}
	}

	if err := srv.Initialize(); err != nil {
		return fmt.Errorf("Initialize() returned an error: %w", err)
	}

	return nil
}

// Note! It create temporary files that should be cleaned up afterwards.
func (srv *RTMPServer) setServerCertificate(
	cert x509.Certificate,
	key crypto.PrivateKey,
) (_err error) {
	var certFile, keyFile *os.File
	defer func() {
		if _err != nil {
			if certFile != nil {
				os.Remove(certFile.Name())
			}
			if keyFile != nil {
				os.Remove(keyFile.Name())
			}
		}
	}()

	var err error
	certFile, err = os.CreateTemp("", "rtmps-server-cert-*.pem")
	if err != nil {
		return fmt.Errorf("unable to create a temporary file for a server certificate: %w", err)
	}
	defer certFile.Close()

	_, err = io.Copy(certFile, bytes.NewReader(cert.Raw))
	if err != nil {
		return fmt.Errorf("unable to write the server certificate to file '%s': %w", certFile.Name())
	}

	keyFile, err = os.CreateTemp("", "rtmps-server-certkey-*.pem")
	if err != nil {
		return fmt.Errorf("unable to create a temporary file for a server certificate: %w", err)
	}
	defer keyFile.Close()

	keyBytes, err := x509.MarshalPKCS8PrivateKey(keyFile)
	if err != nil {
		return fmt.Errorf("unable to serialize into PKCS8 the private key: %w", err)
	}

	_, err = io.Copy(keyFile, bytes.NewReader(keyBytes))
	if err != nil {
		return fmt.Errorf("unable to write the server certificate to file '%s': %w", certFile.Name())
	}

	srv.ServerCert = certFile.Name()
	srv.ServerKey = keyFile.Name()
	return nil
}

var _ types.PortServer = (*RTMPServer)(nil)

func (srv *RTMPServer) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &srv.Locker, func() error {
		if srv.Server == nil {
			return fmt.Errorf("already closed")
		}
		certFile := srv.ServerCert
		keyFile := srv.ServerKey

		srv.Server.Close()
		srv.Server = nil

		var result *multierror.Error
		result = multierror.Append(result, os.Remove(certFile))
		result = multierror.Append(result, os.Remove(keyFile))
		return result.ErrorOrNil()
	})
}
func (srv *RTMPServer) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeRTMP
}
func (srv *RTMPServer) ListenAddr() string {
	return srv.Server.Address
}
func (srv *RTMPServer) NumBytesConsumerWrote() uint64 {
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
func (srv *RTMPServer) NumBytesProducerRead() uint64 {
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
