package streamforward

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamserver/implementations/libav/saferecoder"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/recoder"
)

type Recoder struct {
	*saferecoder.Recoder
	Process *saferecoder.Process
}

func (r *Recoder) StartRecoding(
	ctx context.Context,
	inputIface recoder.Input,
	outputIface recoder.Output,
) error {
	input, ok := inputIface.(*saferecoder.Input)
	if !ok {
		return fmt.Errorf("expected 'input' of type %T, but received %T", input, inputIface)
	}
	output, ok := outputIface.(*saferecoder.Output)
	if !ok {
		return fmt.Errorf("expected 'output' of type %T, but received %T", output, outputIface)
	}
	return r.Recoder.StartRecoding(ctx, input, output)
}

func (r *Recoder) NewInputFromURL(
	ctx context.Context,
	url string,
	cfg recoder.InputConfig,
) (recoder.Input, error) {
	return r.Process.NewInputFromURL(ctx, url, cfg)
}

func (r *Recoder) NewOutputFromURL(
	ctx context.Context,
	url string,
	cfg recoder.OutputConfig,
) (recoder.Output, error) {
	return r.Process.NewOutputFromURL(ctx, url, cfg)
}

func (r *Recoder) WaitForRecordingEnd(ctx context.Context) error {
	return r.Recoder.Wait(ctx)
}

func (r *Recoder) Close() error {
	err := r.Process.Kill()
	r.Process = nil
	r.Recoder = nil
	return err
}
