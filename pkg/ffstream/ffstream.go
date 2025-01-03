package ffstream

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

type FFStream struct{}

func New() *FFStream {
	return &FFStream{}
}

func (s *FFStream) AddInput(input *recoder.Input) {

}

func (s *FFStream) AddOutput(output *recoder.Output) {

}

func (s *FFStream) SetEncoder(encoder *recoder.Loop) {

}

func (s *FFStream) ServeContext(ctx context.Context) {

}
