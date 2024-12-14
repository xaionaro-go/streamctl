package libav

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
	saferecoder "github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder"
)

type Encoder struct {
	*saferecoder.Encoder
	Process *saferecoder.Process
}

func (r *Encoder) StartRecoding(
	ctx context.Context,
	inputIface encoder.Input,
	outputIface encoder.Output,
) error {
	input, ok := inputIface.(*saferecoder.Input)
	if !ok {
		return fmt.Errorf("expected 'input' of type %T, but received %T", input, inputIface)
	}
	output, ok := outputIface.(*saferecoder.Output)
	if !ok {
		return fmt.Errorf("expected 'output' of type %T, but received %T", output, outputIface)
	}
	return r.Encoder.StartEncoding(ctx, input, output)
}

func (r *Encoder) NewInputFromURL(
	ctx context.Context,
	url string,
	authKey string,
	cfg encoder.InputConfig,
) (encoder.Input, error) {
	return r.Process.NewInputFromURL(ctx, url, authKey, cfg)
}

func (r *Encoder) NewOutputFromURL(
	ctx context.Context,
	url string,
	streamKey string,
	cfg encoder.OutputConfig,
) (encoder.Output, error) {
	return r.Process.NewOutputFromURL(ctx, url, streamKey, cfg)
}

func (r *Encoder) WaitForRecordingEnd(ctx context.Context) error {
	return r.Encoder.Wait(ctx)
}

func (r *Encoder) Close() error {
	err := r.Process.Kill()
	r.Process = nil
	r.Encoder = nil
	return err
}
