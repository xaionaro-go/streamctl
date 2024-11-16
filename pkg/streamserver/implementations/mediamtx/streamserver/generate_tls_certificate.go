package streamserver

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"time"
)

func generateServerTLSCertificate() (*x509.Certificate, crypto.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to generate an ED25519 keypair: %w", err)
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
		return nil, nil, fmt.Errorf("unable to generate a certificate: %w", err)
	}

	certificate, err := x509.ParseCertificate(certBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse the certificate that I've just generated: %w", err)
	}

	if certificate.PublicKey == nil {
		return nil, nil, fmt.Errorf("internal error (bug in the code): public key is not set")
	}

	return certificate, privateKey, nil
}
