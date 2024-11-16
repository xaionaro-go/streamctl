package streamserver

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/auth"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	mediamtxlogger "github.com/xaionaro-go/mediamtx/pkg/logger"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/mediamtx/pkg/servers/rtsp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type RTSPServer struct {
	*rtsp.Server

	locker         xsync.Mutex
	originalConfig streamportserver.Config
	isInitialized  bool
}

func newRTSPServer(
	pathManager *pathmanager.PathManager,
	listenAddr string,
	logger mediamtxlogger.Writer,
	opts ...streamportserver.Option,
) (*RTSPServer, error) {
	psCfg := streamportserver.Options(opts).ProtocolSpecificConfig(context.Background())
	srv := &RTSPServer{
		Server: &rtsp.Server{
			Address:           listenAddr,
			AuthMethods:       []auth.ValidateMethod{},
			ReadTimeout:       conf.StringDuration(psCfg.ReadTimeout),
			WriteTimeout:      conf.StringDuration(psCfg.WriteTimeout),
			WriteQueueSize:    int(psCfg.WriteQueueSize),
			UseUDP:            false,
			UseMulticast:      false,
			RTPAddress:        "", // in mediamtx the original value: ":8000"
			RTCPAddress:       "", // in mediamtx the original value: ":8001"
			MulticastIPRange:  "", // in mediamtx the original value: "224.1.0.0/16"
			MulticastRTPPort:  0,  // in mediamtx the original value: 8002
			MulticastRTCPPort: 0,  // in mediamtx the original value: 8003
			IsTLS:             psCfg.IsTLS,
			ServerCert:        "",
			ServerKey:         "",
			RTSPAddress:       listenAddr,
			Protocols: map[conf.Protocol]struct{}{
				conf.Protocol(gortsplib.TransportUDP):          {},
				conf.Protocol(gortsplib.TransportUDPMulticast): {},
				conf.Protocol(gortsplib.TransportTCP):          {},
			},
			RunOnConnect:        "",
			RunOnConnectRestart: false,
			RunOnDisconnect:     "",
			ExternalCmdPool:     nil,
			PathManager:         pathManager,
			Parent:              logger,
		},
		originalConfig: streamportserver.Config{
			ProtocolSpecificConfig: psCfg,
			Type:                   streamtypes.ServerTypeRTSP,
			ListenAddr:             listenAddr,
		},
	}
	if err := srv.init(srv.originalConfig); err != nil {
		return nil, err
	}
	return srv, nil
}

func (srv *RTSPServer) init(
	cfg streamportserver.Config,
) (_err error) {
	defer func() {
		if _err != nil {
			_ = srv.Close()
		}
	}()
	if (cfg.ServerCert != nil) != (cfg.ServerKey != nil) {
		return fmt.Errorf(
			"fields 'ServerCert' and 'ServerKey' should be used together (cur values: ServerCert == %#+v; ServerKey == %#+v)",
			cfg.ServerCert,
			cfg.ServerKey,
		)
	}

	if cfg.ServerCert != nil && cfg.ServerKey != nil {
		err := srv.setServerCertificate(*cfg.ServerCert, cfg.ServerKey)
		if err != nil {
			return fmt.Errorf("unable to set the TLS certificate: %w", err)
		}
	}
	if cfg.IsTLS && (srv.ServerCert == "" || srv.ServerKey == "") {
		ctx := context.TODO()
		logger.Warnf(
			ctx,
			"TLS is enabled, but no certificate is supplied, generating an ephemeral self-signed certificate",
		) // TODO: implement the support of providing the certificates in the UI
		err := srv.generateServerCertificate()
		if err != nil {
			return fmt.Errorf("unable to set the TLS certificate: %w", err)
		}
	}

	if err := srv.Initialize(); err != nil {
		return fmt.Errorf("Initialize() returned an error: %w", err)
	}
	srv.isInitialized = true

	return nil
}

// Note! It create temporary files that should be cleaned up afterwards.
func (srv *RTSPServer) generateServerCertificate() (_err error) {
	certificate, privateKey, err := generateServerTLSCertificate()
	if err != nil {
		return fmt.Errorf("unable to generate the certificate: %w", err)
	}

	err = srv.setServerCertificate(*certificate, privateKey)
	if err != nil {
		return fmt.Errorf("unable to set the TLS certificate to an ephemeral one: %w", err)
	}

	return nil
}

// Note! It create temporary files that should be cleaned up afterwards.
func (srv *RTSPServer) setServerCertificate(
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
	certFile, err = os.CreateTemp("", "rtsps-server-cert-*.pem")
	if err != nil {
		return fmt.Errorf("unable to create a temporary file for a server certificate: %w", err)
	}
	defer certFile.Close()

	certPEM := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	if err := pem.Encode(certFile, certPEM); err != nil {
		return fmt.Errorf(
			"unable to write the server certificate to file '%s' in PEM format: %w",
			certFile.Name(),
			err,
		)
	}

	keyFile, err = os.CreateTemp("", "rtsps-server-certkey-*.pem")
	if err != nil {
		return fmt.Errorf("unable to create a temporary file for a server certificate: %w", err)
	}
	defer keyFile.Close()

	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return fmt.Errorf("unable to serialize into PKCS8 the private key: %w", err)
	}

	privatePem := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	}
	if err := pem.Encode(keyFile, privatePem); err != nil {
		return fmt.Errorf(
			"unable to write the server certificate to file '%s' in PEM format: %w",
			certFile.Name(),
			err,
		)
	}

	srv.ServerCert = certFile.Name()
	srv.ServerKey = keyFile.Name()
	return nil
}

var _ streamportserver.Server = (*RTSPServer)(nil)

func (srv *RTSPServer) ProtocolSpecificConfig() streamportserver.ProtocolSpecificConfig {
	return srv.originalConfig.ProtocolSpecificConfig
}

func (srv *RTSPServer) Close() error {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &srv.locker, func() error {
		if srv.Server == nil {
			return fmt.Errorf("already closed")
		}
		certFile := srv.ServerCert
		keyFile := srv.ServerKey

		if srv.isInitialized {
			srv.Server.Close()
		}
		srv.Server = nil

		var result *multierror.Error
		if certFile != "" {
			result = multierror.Append(result, os.Remove(certFile))
		}
		if keyFile != "" {
			result = multierror.Append(result, os.Remove(keyFile))
		}
		return result.ErrorOrNil()
	})
}
func (srv *RTSPServer) Type() streamtypes.ServerType {
	return streamtypes.ServerTypeRTSP
}
func (srv *RTSPServer) ListenAddr() string {
	return srv.Server.Address
}
func (srv *RTSPServer) NumBytesConsumerWrote() uint64 {
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
func (srv *RTSPServer) NumBytesProducerRead() uint64 {
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
