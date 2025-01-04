package recoder

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/recoder"
)

type InputConfig struct {
	GenericConfig recoder.InputConfig

	CustomOptions []CustomOption
}

type Input struct {
	*astikit.Closer
	*astiav.FormatContext
	*astiav.Dictionary
}

func NewInputFromURL(
	ctx context.Context,
	url string,
	authKey string,
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

	if len(cfg.CustomOptions) > 0 {
		input.Dictionary = astiav.NewDictionary()
		input.Closer.Add(input.Dictionary.Free)

		for _, opt := range cfg.CustomOptions {
			logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			input.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	if err := input.FormatContext.OpenInput(url, nil, input.Dictionary); err != nil {
		return nil, fmt.Errorf("unable to open input by URL '%s': %w", url, err)
	}
	input.Closer.Add(input.FormatContext.CloseInput)

	if err := input.FormatContext.FindStreamInfo(input.Dictionary); err != nil {
		return nil, fmt.Errorf("unable to get stream info: %w", err)
	}
	return input, nil
}
