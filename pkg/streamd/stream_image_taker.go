package streamd

import (
	"context"
	"fmt"
	"net/url"
	"sort"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/recoder"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

func (p *imageDataProvider) newStreamImageTaker(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (_ret *streamImageTaker, _err error) {
	/*factory, err := libav.NewFactory(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a libav factory: %w", err)
	}

	r, err := factory.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get a recoder factory: %w", err)
	}
	defer func() {
		if _err != nil {
			r.Close()
		}
	}()

	myURL, err := getLocalhostEndpoint(ctx, p.StreamD.StreamServer)
	if err != nil {
		return nil, fmt.Errorf("unable to get an URL to myself: %w", err)
	}
	myURL.Path = string(streamSourceID)

	input, err := r.NewInputFromURL(ctx, myURL.String(), "", recoder.InputConfig{})
	if err != nil {
		return nil, fmt.Errorf("unable to open URL '%s': %w", myURL.String(), err)
	}
	defer func() {
		if _err != nil {
			input.Close()
		}
	}()*/

	return nil, fmt.Errorf("not implemented")
}

type streamImageTaker struct {
}

func (p *streamImageTaker) Keepalive() bool {
	panic("not implemented")
}

func (p *streamImageTaker) GetLastFrame(
	ctx context.Context,
) ([]byte, recoder.VideoCodec, error) {
	return nil, 0, fmt.Errorf("not implemented")
}

func getLocalhostEndpoint(
	ctx context.Context,
	streamServer streamportserver.GetPortServerser,
) (_ret *url.URL, _err error) {
	defer func() { logger.Debugf(ctx, "getLocalhostEndpoint result: %v %v", _ret, _err) }()

	portSrvs, err := streamServer.GetPortServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get port servers info: %w", err)
	}

	sort.Slice(portSrvs, func(i, j int) bool {
		a := &portSrvs[i]
		b := &portSrvs[j]
		if a.IsTLS != b.IsTLS {
			return b.IsTLS
		}
		return false
	})
	portSrv := portSrvs[0]
	logger.Debugf(ctx, "getLocalhostEndpoint: chosen portSrv == %#+v", portSrv)

	protoString := portSrv.Type.String()
	if portSrv.IsTLS {
		protoString += "s"
	}
	urlString := fmt.Sprintf("%s://%s", protoString, portSrv.ListenAddr)
	urlParsed, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse '%s': %w", urlString, err)
	}

	return urlParsed, nil
}
