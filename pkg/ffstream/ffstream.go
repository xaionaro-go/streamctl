package ffstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/xaionaro-go/libsrt/threadsafe"
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

func (s *FFStream) RemoveOutput(
	ctx context.Context,
	outputID recoder.OutputID,
) error {
	s.locker.Lock()
	defer s.locker.Unlock()
	if s.Output == nil {
		return fmt.Errorf("there are no outputs right now")
	}
	if s.Output.ID != outputID {
		return fmt.Errorf("there are no outputs with ID %d", outputID)
	}
	err := s.RecoderLoop.RemoveOutput(ctx, outputID)
	s.Output.Close()
	s.Output = nil
	return err
}

func (s *FFStream) GetEncoderConfig(
	ctx context.Context,
) EncoderConfig {
	return s.Encoder.Config
}

func (s *FFStream) SetEncoderConfig(
	ctx context.Context,
	cfg EncoderConfig,
) error {
	return s.Encoder.Configure(ctx, cfg)
}

func (s *FFStream) GetEncoderStats(
	ctx context.Context,
) *recoder.EncoderStatistics {
	return s.Encoder.GetStats()
}

func (s *FFStream) WithSRTOutput(
	ctx context.Context,
	callback func(*threadsafe.Socket) error,
) error {
	sock, err := s.Output.SRT(ctx)
	if err != nil {
		return fmt.Errorf("unable to get the SRT socket handler: %w", err)
	}

	return callback(sock)
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
