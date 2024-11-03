package weron

import (
	"context"
	"net"
	"sync/atomic"
)

type dummyListener struct {
	conn        net.Conn
	ctx         context.Context
	acceptCount atomic.Uint32
}

func newDummyListener(ctx context.Context, conn net.Conn) net.Listener {
	return &dummyListener{conn: conn, ctx: ctx}
}

func (l *dummyListener) Accept() (net.Conn, error) {
	if l.acceptCount.Add(1) > 1 {
		<-l.ctx.Done()
		return nil, l.ctx.Err()
	}

	return l.conn, nil
}

func (l *dummyListener) Close() error {
	return nil
}

func (l *dummyListener) Addr() net.Addr {
	lAddr := l.conn.LocalAddr()
	if lAddr == nil {
		panic("l.conn.LocalAddr() is nil")
	}
	return lAddr
}
