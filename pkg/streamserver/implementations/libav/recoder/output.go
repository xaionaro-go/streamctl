package recoder

import (
	"context"
	"fmt"
	"log"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
)

type OutputConfig struct{}

type Output struct {
	*astikit.Closer
	*astiav.FormatContext
}

func NewOutputFromURL(
	ctx context.Context,
	url string,
	cfg OutputConfig,
) (*Output, error) {
	if url == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	output := &Output{
		Closer: astikit.NewCloser(),
	}

	formatContext, err := astiav.AllocOutputFormatContext(nil, "", url)
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
		logger.Tracef(ctx, "destination '%s' is a file", url)
		ioContext, err := astiav.OpenIOContext(url, astiav.NewIOContextFlags(astiav.IOContextFlagWrite))
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
