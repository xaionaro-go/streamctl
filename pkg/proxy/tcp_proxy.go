package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"

	"github.com/xaionaro-go/observability"
)

type TCPProxy struct {
	config       TCPConfig
	wg           sync.WaitGroup
	dstAddr      string
	stopListener context.CancelFunc
}

type TCPConfig struct {
	DestinationIsTLS     bool
	DestinationTLSConfig *tls.Config
}

func NewTCP(dstAddr string, config *TCPConfig) *TCPProxy {
	if config == nil {
		config = &TCPConfig{}
	}
	return &TCPProxy{
		dstAddr: dstAddr,
		config:  *config,
	}
}

func (p *TCPProxy) ListenRandomPort(
	ctx context.Context,
) (*net.TCPAddr, error) {
	if p.stopListener != nil {
		return nil, fmt.Errorf("the proxy is already listening another port")
	}

	const localhost = "127.0.0.1"
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{
		IP:   net.ParseIP(localhost),
		Port: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to start listening a random port at '%s': %w", localhost, err)
	}
	resultingAddr := listener.Addr().(*net.TCPAddr)
	ctx, cancelFunc := context.WithCancel(ctx)
	p.stopListener = cancelFunc
	p.wg.Add(1)
	observability.Go(ctx, func() {
		defer p.wg.Done()
		<-ctx.Done()
		listener.Close()
	})
	p.wg.Add(1)
	observability.Go(ctx, func() {
		defer p.wg.Done()
		err := p.serve(ctx, listener)
		if err != nil {
			logger.Errorf(ctx, "unable to serve at '%s': %v", resultingAddr, err)
		}
	})
	return resultingAddr, nil
}

// Serve accepts and handles the connections from the listener, and
// it takes ownership of the listener (it will be closed when TLSProxyTLS
// will be closed)
func (p *TCPProxy) serve(
	ctx context.Context,
	listener net.Listener,
) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return fmt.Errorf("unable to accept a connection: %w", err)
		}

		p.wg.Add(1)
		observability.Go(ctx, func() {
			defer p.wg.Done()
			err := p.handleConnection(ctx, conn)
			if err != nil {
				logger.Errorf(ctx, "unable to handle connection %s->%s: %v", conn.LocalAddr(), conn.RemoteAddr(), err)
				_ = p.Close()
			}
		})
	}
}

func (p *TCPProxy) Close() error {
	if p.stopListener == nil {
		return fmt.Errorf("the proxy is not opened, and thus cannot be closed")
	}
	p.stopListener()
	p.stopListener = nil
	p.wg.Wait()
	return nil
}

func (p *TCPProxy) handleConnection(
	ctx context.Context,
	conn net.Conn,
) error {
	var (
		dstConn net.Conn
		err     error
	)
	if p.config.DestinationIsTLS {
		dstConn, err = tls.Dial("tcp", p.dstAddr, p.config.DestinationTLSConfig)
	} else {
		dstConn, err = net.Dial("tcp", p.dstAddr)
	}
	if err != nil {
		return fmt.Errorf("unable to dial destination '%s' (isTLS: %v): %w", p.dstAddr, p.config.DestinationIsTLS, err)
	}

	err = proxyConns(ctx, conn, dstConn)
	if err != nil {
		return fmt.Errorf("unable to proxy connections %s->%s <-> %s->%s: %w", conn.LocalAddr(), conn.RemoteAddr(), dstConn.LocalAddr(), dstConn.RemoteAddr(), err)
	}

	return nil
}

func proxyConns(
	ctx context.Context,
	srcConn net.Conn,
	dstConn net.Conn,
) error {
	ctx, cancelFunc := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		err := pipeConn(srcConn, dstConn)
		if err != nil {
			cancelFunc()
			logger.Error(ctx, "unable to pipe traffic from %s->%s to %s->%s: %v", srcConn.LocalAddr(), srcConn.RemoteAddr(), srcConn.LocalAddr(), dstConn.RemoteAddr(), err)
		}
	})
	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		err := pipeConn(dstConn, srcConn)
		if err != nil {
			cancelFunc()
			logger.Error(ctx, "unable to pipe traffic from %s->%s to %s->%s: %v", dstConn.RemoteAddr(), dstConn.LocalAddr(), srcConn.RemoteAddr(), srcConn.LocalAddr(), err)
		}
	})

	<-ctx.Done()
	_ = srcConn.Close()
	_ = dstConn.Close()
	wg.Wait()
	return nil
}

func pipeConn(
	from, to net.Conn,
) error {
	buf := make([]byte, 1<<10)
	for {
		r, err := from.Read(buf)
		if err != nil {
			return fmt.Errorf("unable to read from the source: %w", err)
		}
		packet := buf[:r]

		w, err := to.Write(packet)
		if err != nil {
			return fmt.Errorf("unable to write to the output: %w", err)
		}
		if w < r {
			return fmt.Errorf("only %d bytes of %d were written", w, r)
		}
	}
}
