package recoder

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
)

type InputConfig struct{}

type Input struct {
	*astikit.Closer
	*astiav.FormatContext
}

func NewInputFromURL(
	ctx context.Context,
	url string,
	cfg InputConfig,
) (*Input, error) {
	if url == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	input := &Input{
		Closer: astikit.NewCloser(),
	}

	input.FormatContext = astiav.AllocFormatContext()
	if input.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}
	input.Closer.Add(input.FormatContext.Free)

	if err := input.FormatContext.OpenInput(url, nil, nil); err != nil {
		return nil, fmt.Errorf("unable to open input by URL '%s': %w", url, err)
	}
	input.Closer.Add(input.FormatContext.CloseInput)

	if err := input.FormatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("unable to get stream info: %w", err)
	}
	return input, nil
}
