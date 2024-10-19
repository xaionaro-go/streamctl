package recoder

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type OutputConfig = recoder.OutputConfig

type Output struct {
	*astikit.Closer
	*astiav.FormatContext
}

func formatFromScheme(scheme string) string {
	switch scheme {
	case "rtmp", "rtmps":
		return "flv"
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
		if !strings.HasSuffix(url.Path, "/") {
			url.Path += "/"
		}
		url.Path += streamKey
	}

	output := &Output{
		Closer: astikit.NewCloser(),
	}

	formatContext, err := astiav.AllocOutputFormatContext(
		nil,
		formatFromScheme(url.Scheme),
		url.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("allocating output format context failed: %w", err)
	}
	if formatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate the output format context")
	}
	output.FormatContext = formatContext
	output.Closer.Add(output.FormatContext.Free)

	// if output is a file:
	if !output.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		logger.Tracef(ctx, "destination '%s' is a file", url.String())
		ioContext, err := astiav.OpenIOContext(
			url.String(),
			astiav.NewIOContextFlags(astiav.IOContextFlagWrite),
		)
		if err != nil {
			log.Fatal(fmt.Errorf("main: opening io context failed: %w", err))
		}
		output.Closer.Add(func() {
			err := ioContext.Close()
			if err != nil {
				logger.Errorf(ctx, "unable to close the IO context: %w", err)
			}
		})
		output.FormatContext.SetPb(ioContext)
	}

	return output, nil
}
