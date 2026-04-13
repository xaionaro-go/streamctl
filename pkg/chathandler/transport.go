package chathandler

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const tlsProbeTimeout = 3 * time.Second

// DetectTransportCredentials probes the server with a TLS handshake.
// If the handshake succeeds, it returns TLS credentials (with InsecureSkipVerify
// for self-signed certs). Otherwise it returns insecure plaintext credentials.
func DetectTransportCredentials(
	ctx context.Context,
	addr string,
) grpc.DialOption {
	probeCtx, cancel := context.WithTimeout(ctx, tlsProbeTimeout)
	defer cancel()

	dialer := tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	conn, err := dialer.DialContext(probeCtx, "tcp", addr)
	if err != nil {
		logger.Debugf(ctx, "TLS probe to %s failed, using plaintext: %v", addr, err)
		return grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	conn.Close()

	logger.Debugf(ctx, "TLS probe to %s succeeded, using TLS", addr)
	return grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		InsecureSkipVerify: true,
	}))
}
