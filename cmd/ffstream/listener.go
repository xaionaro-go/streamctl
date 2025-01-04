package main

import (
	"context"
	"net"
	"strings"
)

func getListener(
	ctx context.Context,
	addr string,
) (net.Listener, error) {
	parts := strings.SplitN(addr, ":", 2)

	if len(parts) == 1 {
		return net.Listen("unixpacket", addr)
	}

	switch parts[0] {
	case "tcp", "tcp4", "tcp6", "udp", "udp4", "udp6", "unix", "unixpacket":
		return net.Listen(parts[0], parts[1])
	}

	return net.Listen("tcp", addr)
}
