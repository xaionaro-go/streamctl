package ffstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

type FFStream struct {
	locker      sync.Mutex
	RecoderLoop *recoder.Loop
	Encoder     *Encoder
	Input       *recoder.Input
	Output      *recoder.Output
}

func New() *FFStream {
	return &FFStream{
		RecoderLoop: recoder.NewLoop(),
		Encoder:     NewEncoder(),
	}
}

func (s *FFStream) AddInput(
	ctx context.Context,
	input *recoder.Input,
) error {
	s.locker.Lock()
	defer s.locker.Unlock()
	if s.Input != nil {
		return fmt.Errorf("currently we support only one input")
	}
	s.Input = input
	return s.RecoderLoop.AddInput(ctx, input)
}

func (s *FFStream) AddOutput(
	ctx context.Context,
	output *recoder.Output,
) error {
	s.locker.Lock()
	defer s.locker.Unlock()
	if s.Output != nil {
		return fmt.Errorf("currently we support only one output")
	}
	s.Output = output
	return s.RecoderLoop.AddOutput(ctx, output)
}

func (s *FFStream) ConfigureEncoder(
	ctx context.Context,
	cfg EncoderConfig,
) error {
	return s.Encoder.Configure(ctx, cfg)
}

func (s *FFStream) GetEncoderStats(
	ctx context.Context,
) *recoder.CommonsEncoderStatistics {
	return s.Encoder.GetStats()
}

func (s *FFStream) GetOutputSRTStats(
	ctx context.Context,
) (*recoder.SRTStats, error) {
	return s.Output.GetSRTStats()
}

func (s *FFStream) Start(
	ctx context.Context,
) error {
	return s.RecoderLoop.Start(ctx, s.Encoder)
}

func (s *FFStream) Wait(
	ctx context.Context,
) error {
	return s.RecoderLoop.Wait(ctx)
}
