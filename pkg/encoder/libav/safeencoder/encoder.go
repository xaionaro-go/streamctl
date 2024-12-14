package safeencoder

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/encoder"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/encoder/types"
	"github.com/xaionaro-go/streamctl/pkg/encoder/libav/safeencoder/process"
)

type Packet = types.Packet

type EncoderID = process.EncoderID
type EncoderConfig = process.EncoderConfig

type Encoder struct {
	Process *Process
	ID      EncoderID
}

func (p *Process) NewEncoder(
	ctx context.Context,
) (*Encoder, error) {
	recoderID, err := p.processBackend.Client.NewEncoder(ctx)
	if err != nil {
		return nil, err
	}
	return &Encoder{
		Process: p,
		ID:      EncoderID(recoderID),
	}, nil
}

func (r *Encoder) Encode(
	ctx context.Context,
	input *Input,
	output *Output,
) error {
	err := r.StartEncoding(ctx, input, output)
	if err != nil {
		return fmt.Errorf("got an error while starting the recording: %w", err)
	}

	if err != r.Wait(ctx) {
		return fmt.Errorf("got an error while waiting for a completion: %w", err)
	}

	return nil
}

func (r *Encoder) StartEncoding(
	ctx context.Context,
	input *Input,
	output *Output,
) error {
	return r.Process.processBackend.Client.StartEncoding(
		ctx,
		r.ID,
		input.ID,
		output.ID,
	)
}

type EncoderStats = encoder.Stats

func (r *Encoder) GetStats(ctx context.Context) (*EncoderStats, error) {
	return r.Process.processBackend.Client.GetEncoderStats(ctx, r.ID)
}

func (r *Encoder) Wait(ctx context.Context) error {
	ch, err := r.Process.Client.EncodingEndedChan(ctx, r.ID)
	if err != nil {
		return err
	}
	<-ch
	return nil
}
