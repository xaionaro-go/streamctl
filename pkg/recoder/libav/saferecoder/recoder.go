package saferecoder

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/recoder"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder/types"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/saferecoder/process"
)

type Packet = types.Packet

type RecoderID = process.RecoderID
type EncoderID = process.EncoderID
type EncoderConfig = process.EncoderConfig

type Recoder struct {
	Process *Process
	ID      RecoderID
}

func (p *Process) NewRecoder(
	ctx context.Context,
) (*Recoder, error) {
	recoderID, err := p.processBackend.Client.NewRecoder(ctx)
	if err != nil {
		return nil, err
	}
	return &Recoder{
		Process: p,
		ID:      RecoderID(recoderID),
	}, nil
}

func (r *Recoder) Recode(
	ctx context.Context,
	encoder *Encoder,
	input *Input,
	output *Output,
) error {
	err := r.StartRecoding(ctx, encoder, input, output)
	if err != nil {
		return fmt.Errorf("got an error while starting the recording: %w", err)
	}

	if err != r.Wait(ctx) {
		return fmt.Errorf("got an error while waiting for a completion: %w", err)
	}

	return nil
}

func (r *Recoder) StartRecoding(
	ctx context.Context,
	encoder *Encoder,
	input *Input,
	output *Output,
) error {
	return r.Process.processBackend.Client.StartRecoding(ctx, r.ID, encoder.ID, input.ID, output.ID)
}

func (r *Recoder) SetEncoderConfig(
	ctx context.Context,
	encoderID EncoderID,
	cfg EncoderConfig,
) error {
	return r.Process.processBackend.Client.SetEncoderConfig(ctx, encoderID, cfg)
}

type EncoderStats = recoder.Stats

func (r *Recoder) GetStats(ctx context.Context) (*EncoderStats, error) {
	return r.Process.processBackend.Client.GetRecoderStats(ctx, r.ID)
}

func (r *Recoder) Wait(ctx context.Context) error {
	ch, err := r.Process.Client.RecodingEndedChan(ctx, r.ID)
	if err != nil {
		return err
	}
	<-ch
	return nil
}
