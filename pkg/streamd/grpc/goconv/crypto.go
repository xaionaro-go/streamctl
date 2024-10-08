package goconv

import (
	"crypto"
	"crypto/x509"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func TLSCertificateGRPC2Go(cert *streamd_grpc.TLSCertificate) (*x509.Certificate, error) {
	if cert == nil {
		return nil, nil
	}
	switch v := cert.TLSCertificateOneOf.(type) {
	case *streamd_grpc.TLSCertificate_X509:
		return CertificateX509GRPC2Go(v)
	default:
		return nil, fmt.Errorf("unexpected type %T", v)
	}
}

func CertificateX509GRPC2Go(cert *streamd_grpc.TLSCertificate_X509) (*x509.Certificate, error) {
	if cert == nil {
		return nil, nil
	}

	return x509.ParseCertificate(cert.X509)
}

func PrivateKeyGRPC2Go(privKey *streamd_grpc.PrivateKey) (crypto.PrivateKey, error) {
	if privKey == nil {
		return nil, nil
	}
	switch v := privKey.PrivateKeyOneOf.(type) {
	case *streamd_grpc.PrivateKey_PKCS8:
		return PrivateKeyPKCS8GRPC2Go(v)
	default:
		return nil, fmt.Errorf("unexpected type %T", v)
	}
}

func PrivateKeyPKCS8GRPC2Go(privKey *streamd_grpc.PrivateKey_PKCS8) (crypto.PrivateKey, error) {
	if privKey == nil {
		return nil, nil
	}

	return x509.ParsePKCS8PrivateKey(privKey.PKCS8)
}
