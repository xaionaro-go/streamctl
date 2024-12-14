package xaionarogortmp

import (
	"context"
	"fmt"
	"net/url"
	"runtime/debug"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/go-rtmp"
	rtmpmsg "github.com/xaionaro-go/go-rtmp/message"
	"github.com/xaionaro-go/streamctl/pkg/encoder"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type Output struct {
	xsync.Mutex
	Client    *rtmp.ClientConn
	StreamKey string
}

var _ encoder.Output = (*Output)(nil)

func (r *Encoder) NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey string,
	cfg encoder.OutputConfig,
) (_ encoder.Output, _err error) {
	var output *Output
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got panic: %v", r)
		}
		if _err == nil {
			return
		}
		logger.FromCtx(ctx).
			WithField("error_event_exception_stack_trace", string(debug.Stack())).Errorf("%v", _err)
		output.Close()
	}()

	client, err := newRTMPClient(ctx, urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to the output endpoint '%s': %w", urlString, err)
	}
	output.Client = client

	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
	}

	if url.Scheme == "rtmp" && url.Port() == "1935" {
		url.Host = url.Hostname()
	}
	output.StreamKey = streamKey

	if err := client.Connect(ctx, &rtmpmsg.NetConnectionConnect{
		Command: rtmpmsg.NetConnectionConnectCommand{
			App:      strings.Trim(url.Path, "/"),
			Type:     "nonprivate",
			FlashVer: "StreamPanel",
			TCURL:    url.String(),
		},
	}); err != nil {
		return nil, fmt.Errorf("unable to connect the endpoint '%s': %w", urlString, err)
	}
	logger.Debugf(ctx, "connected the stream to '%s'", urlString)

	return output, nil
}

func (output *Output) Close() error {
	var err *multierror.Error
	ctx := context.TODO()
	output.Do(ctx, func() {
		if output.Client != nil {
			err = multierror.Append(
				err,
				output.Client.Close(),
			)
			output.Client = nil
		}
	})
	return err.ErrorOrNil()
}
