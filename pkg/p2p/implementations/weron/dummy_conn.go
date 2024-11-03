package weron

import (
	"fmt"
	"io"
	"net"
	"time"
)

type dummyConn struct {
	wrc   io.ReadWriteCloser
	lAddr net.Addr
}

func connFromReadWriter(
	wrc io.ReadWriteCloser,
	lAddr net.Addr,
) net.Conn {
	return &dummyConn{
		wrc:   wrc,
		lAddr: lAddr,
	}
}

func (c *dummyConn) Read(b []byte) (n int, err error) {
	return c.wrc.Read(b)
}
func (c *dummyConn) Write(b []byte) (n int, err error) {
	return c.wrc.Write(b)
}
func (c *dummyConn) Close() error {
	return c.wrc.Close()
}
func (c *dummyConn) LocalAddr() net.Addr {
	return c.lAddr
}
func (c *dummyConn) RemoteAddr() net.Addr {
	return nil
}
func (c *dummyConn) SetDeadline(t time.Time) error {
	return fmt.Errorf("SetDeadline: not implemented")
}
func (c *dummyConn) SetReadDeadline(t time.Time) error {
	return fmt.Errorf("SetReadDeadline: not implemented")
}
func (c *dummyConn) SetWriteDeadline(t time.Time) error {
	return fmt.Errorf("SetWriteDeadline: not implemented")
}
