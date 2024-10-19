package streamserver

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/mediamtx/pkg/conf"
	mediamtxlogger "github.com/xaionaro-go/mediamtx/pkg/logger"
	"github.com/xaionaro-go/mediamtx/pkg/pathmanager"
	"github.com/xaionaro-go/mediamtx/pkg/servers/rtmp"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type RTMPServer struct {
	*rtmp.Server

	locker         xsync.Mutex
	originalConfig streamportserver.Config
	isInitialized  bool
}

func newRTMPServer(
	pathManager *pathmanager.PathManager,
	listenAddr string,
	logger mediamtxlogger.Writer,
	opts ...streamportserver.Option,
) (*RTMPServer, error) {
	psCfg := streamportserver.Options(opts).ProtocolSpecificConfig(context.Background())
	srv := &RTMPServer{
		originalConfig: streamportserver.Config{
			ProtocolSpecificConfig: psCfg,
			Type:                   streamtypes.ServerTypeRTMP,
			ListenAddr:             listenAddr,
		},
		Server: &rtmp.Server{
			Address:             listenAddr,
			ReadTimeout:         conf.StringDuration(psCfg.ReadTimeout),
			WriteTimeout:        conf.StringDuration(psCfg.WriteTimeout),
			IsTLS:               psCfg.IsTLS,
			ServerCert:          "",
			ServerKey:           "",
			RTSPAddress:         "",
			RunOnConnect:        "",
			RunOnConnectRestart: false,
			RunOnDisconnect:     "",
			ExternalCmdPool:     nil,
			PathManager:         pathManager,
			Parent:              logger,
		},
	}
	if err := srv.init(srv.originalConfig); err != nil {
		return nil, err
	}
	return srv, nil
}

func (srv *RTMPServer) init(
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
func (srv *RTMPServer) generateServerCertificate() (_err error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("unable to generate an ED25519 keypair: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		log.Fatalf("Error generating serial number: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"StreamPanel"},
		},
		NotBefore:             time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		return fmt.Errorf("unable to generate a certificate: %w", err)
	}

	certificate, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return fmt.Errorf("unable to parse the certificate that I've just generated: %w", err)
	}

	if certificate.PublicKey == nil {
		return fmt.Errorf("internal error (bug in the code): public key is not set")
	}

	err = srv.setServerCertificate(*certificate, privateKey)
	if err != nil {
		return fmt.Errorf("unable to set the TLS certificate to an ephemeral one: %w", err)
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

	keyFile, err = os.CreateTemp("", "rtmps-server-certkey-*.pem")
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

var _ streamportserver.Server = (*RTMPServer)(nil)

func (srv *RTMPServer) ProtocolSpecificConfig() streamportserver.ProtocolSpecificConfig {
	return srv.originalConfig.ProtocolSpecificConfig
}

func (srv *RTMPServer) Close() error {
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
