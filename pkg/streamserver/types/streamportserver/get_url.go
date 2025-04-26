package streamportserver

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func GetURLForStreamID(
	ctx context.Context,
	streamServer GetPortServerser,
	streamID streamtypes.StreamID,
) (*url.URL, error) {
	portSrvs, err := streamServer.GetPortServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of stream server ports: %w", err)
	}
	if len(portSrvs) == 0 {
		return nil, fmt.Errorf("there are no open server ports")
	}
	portSrv := portSrvs[0]

	var u url.URL
	u.Scheme = portSrv.Type.String()
	u.Host = portSrv.ListenAddr
	_, port, _ := net.SplitHostPort(portSrv.ListenAddr)
	switch u.Hostname() {
	case "0.0.0.0":
		u.Host = net.JoinHostPort("127.0.0.1", port)
	case "::":
		u.Host = net.JoinHostPort("::1", port)
	}
	switch portSrv.Type {
	case streamtypes.ServerTypeSRT:
		u.RawQuery = fmt.Sprintf("streamid=read:%s&latency=%d",
			streamID,
			500_000,
		)
	default:
		u.Path = string(streamID)
	}
	return &u, nil
}
