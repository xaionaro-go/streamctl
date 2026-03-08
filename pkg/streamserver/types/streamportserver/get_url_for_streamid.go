package streamportserver

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func GetPreferredPortServer(
	ctx context.Context,
	streamServer GetPortServerser,
	preferenceLessFunc func(*Config, *Config) bool,
) (*Config, error) {
	portSrvs, err := streamServer.GetPortServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of stream server ports: %w", err)
	}
	if len(portSrvs) == 0 {
		return nil, fmt.Errorf("there are no open server ports")
	}

	sort.Slice(portSrvs, func(i, j int) bool {
		a := &portSrvs[i]
		b := &portSrvs[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.IsTLS != b.IsTLS {
			return b.IsTLS
		}
		return false
	})
	if preferenceLessFunc != nil {
		sort.SliceStable(portSrvs, func(i, j int) bool {
			a := &portSrvs[i]
			b := &portSrvs[j]
			return preferenceLessFunc(b, a)
		})
		logger.Debugf(ctx, "resulting slice: %#+v", portSrvs)
	}
	portSrv := portSrvs[0]
	return &portSrv, nil
}

func GetURLForLocalStreamID(
	ctx context.Context,
	streamServer GetPortServerser,
	streamSourceID streamtypes.StreamSourceID,
	preferenceLessFunc func(*Config, *Config) bool,
) (*url.URL, error) {
	return GetURLForRemoveStreamSourceID(ctx, "127.0.0.1", "::1", streamServer, streamSourceID, preferenceLessFunc)
}

func GetURLForRemoveStreamSourceID(
	ctx context.Context,
	streamDAddrV4 string,
	streamDAddrV6 string,
	streamServer GetPortServerser,
	streamSourceID streamtypes.StreamSourceID,
	preferenceLessFunc func(*Config, *Config) bool,
) (_ret *url.URL, _err error) {
	logger.Debugf(ctx, "GetURLForRemoveStreamSourceID(ctx, '%s', '%s', %T, '%s', %p)", streamDAddrV4, streamDAddrV6, streamServer, streamSourceID, preferenceLessFunc)
	defer func() {
		logger.Debugf(ctx, "/GetURLForRemoveStreamSourceID(ctx, '%s', '%s', %T, '%s', %p): %v %v", streamDAddrV4, streamDAddrV6, streamServer, streamSourceID, preferenceLessFunc, _ret, _err)
	}()

	portSrv, err := GetPreferredPortServer(ctx, streamServer, preferenceLessFunc)
	if err != nil {
		return nil, fmt.Errorf("unable to get the port server: %w", err)
	}

	protoString := portSrv.Type.String()
	if portSrv.IsTLS {
		protoString += "s"
	}

	var u url.URL
	u.Scheme = protoString
	u.Host = portSrv.ListenAddr
	_, port, _ := net.SplitHostPort(portSrv.ListenAddr)
	switch u.Hostname() {
	case "0.0.0.0":
		u.Host = net.JoinHostPort(streamDAddrV4, port)
	case "::":
		u.Host = net.JoinHostPort(streamDAddrV6, port)
	}
	switch portSrv.Type {
	case streamtypes.ServerTypeSRT:
		u.RawQuery = fmt.Sprintf("streamid=read:%s&latency=%d",
			streamSourceID,
			500_000,
		)
	default:
		u.Path = string(streamSourceID)
	}
	return &u, nil
}
