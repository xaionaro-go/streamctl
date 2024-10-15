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
type RecoderConfig = process.RecoderConfig

type Recoder struct {
	Process *Process
	ID      RecoderID
}

func (p *Process) NewRecoder(
	cfg RecoderConfig,
) (*Recoder, error) {
	recoderID, err := p.processBackend.Client.NewRecoder(context.TODO(), cfg)
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
	input *Input,
	output *Output,
) error {
	err := r.StartRecoding(ctx, input, output)
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
	input *Input,
	output *Output,
) error {
	return r.Process.processBackend.Client.StartRecoding(
		context.TODO(),
		r.ID,
		input.ID,
		output.ID,
	)
}

type RecoderStats = recoder.Stats

func (r *Recoder) GetStats(ctx context.Context) (*RecoderStats, error) {
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
