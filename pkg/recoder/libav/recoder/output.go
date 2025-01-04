package recoder

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/proxy"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

const unwrapTLSViaProxy = false

type OutputConfig struct {
	recoder.OutputConfig

	CustomOptions []CustomOption
}

type Output struct {
	*astikit.Closer
	*astiav.FormatContext
	*astiav.Dictionary
}

func formatFromScheme(scheme string) string {
	switch scheme {
	case "rtmp", "rtmps":
		return "flv"
	case "srt":
		return "mpegts"
	default:
		return scheme
	}
}

func NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey string,
	cfg OutputConfig,
) (*Output, error) {
	if urlString == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", url, err)
	}

	if streamKey != "" {
		switch {
		case url.Path == "" || url.Path == "/":
			url.Path = "//"
		case !strings.HasSuffix(url.Path, "/"):
			url.Path += "/"
		}
		url.Path += streamKey
	}

	if url.Port() == "" {
		switch url.Scheme {
		case "rtmp":
			url.Host += ":1935"
		case "rtmps":
			url.Host += ":443"
		}
	}

	needUnwrapTLSFor := ""
	switch url.Scheme {
	case "rtmps":
		needUnwrapTLSFor = "rtmp"
	}

	output := &Output{
		Closer: astikit.NewCloser(),
	}

	if needUnwrapTLSFor != "" && unwrapTLSViaProxy {
		proxy := proxy.NewTCP(url.Host, &proxy.TCPConfig{
			DestinationIsTLS: true,
		})
		proxyAddr, err := proxy.ListenRandomPort(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to make a TLS-proxy: %w", err)
		}
		output.Closer.Add(func() {
			err := proxy.Close()
			if err != nil {
				logger.Errorf(ctx, "unable to close the TLS-proxy: %v", err)
			}
		})
		url.Scheme = needUnwrapTLSFor
		url.Host = proxyAddr.String()
	}

	formatName := formatFromScheme(url.Scheme)

	if len(cfg.CustomOptions) > 0 {
		output.Dictionary = astiav.NewDictionary()
		output.Closer.Add(output.Dictionary.Free)

		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				formatName = opt.Value
				continue
			}
			logger.Debugf(ctx, "output.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			output.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	logger.Debugf(observability.OnInsecureDebug(ctx), "URL: %s", url)
	formatContext, err := astiav.AllocOutputFormatContext(
		nil,
		formatName,
		url.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("allocating output format context failed using URL '%s': %w", url, err)
	}
	if formatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate the output format context")
	}
	output.FormatContext = formatContext
	output.Closer.Add(output.FormatContext.Free)

	if output.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		// if output is not a file then nothing else to do
		return output, nil
	}
	logger.Tracef(ctx, "destination '%s' is a file", url)

	ioContext, err := astiav.OpenIOContext(
		url.String(),
		astiav.NewIOContextFlags(astiav.IOContextFlagWrite),
		nil,
		output.Dictionary,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to open IO context (URL: '%s'): %w", url, err)
	}

	output.Closer.Add(func() {
		err := ioContext.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the IO context (URL: %s): %v", url, err)
		}
	})
	output.FormatContext.SetPb(ioContext)

	return output, nil
}
