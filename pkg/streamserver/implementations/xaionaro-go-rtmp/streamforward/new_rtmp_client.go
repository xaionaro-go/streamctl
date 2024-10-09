package streamforward

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/go-rtmp"
	"github.com/xaionaro-go/streamctl/pkg/xlogger"
)

func newRTMPClient(
	ctx context.Context,
	url url.URL,
) (*rtmp.ClientConn, error) {
	logger.Debugf(ctx, "connecting to '%s'", url.String())
	if url.Port() == "" {
		switch url.Scheme {
		case "rtmp":
			url.Host += ":1935"
		case "rtmps":
			url.Host += ":443"
		default:
			return nil, fmt.Errorf("unexpected scheme '%s' in URL '%s'", url.Scheme, url.String())
		}
	}
	var dialFunc func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error)
	switch url.Scheme {
	case "rtmp":
		dialFunc = rtmp.Dial
	case "rtmps":
		dialFunc = func(protocol, addr string, config *rtmp.ConnConfig) (*rtmp.ClientConn, error) {
			return rtmp.TLSDial(protocol, addr, config, http.DefaultTransport.(*http.Transport).TLSClientConfig)
		}
	default:
		return nil, fmt.Errorf("unexpected scheme '%s' in URL '%s'", url.Scheme, url.String())
	}
	client, err := dialFunc(url.Scheme, url.Host, &rtmp.ConnConfig{
		Logger: xlogger.LogrusFieldLoggerFromCtx(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to connect to '%s': %w", url.String(), err)
	}

	return client, nil
}
