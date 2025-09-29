package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/secret"
	llms "github.com/xaionaro-go/streamctl/pkg/llm"
	llmtypes "github.com/xaionaro-go/streamctl/pkg/llm/types"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xsync"
)

type llm struct {
	locker     xsync.Mutex
	streamD    *StreamD
	cancelFunc context.CancelFunc
	backend    llmtypes.LLM
}

func newLLM(d *StreamD) *llm {
	return &llm{streamD: d}
}

func (d *StreamD) initLLMs(
	ctx context.Context,
) error {
	d.llm = newLLM(d)
	d.llm.UpdateConfig(ctx)
	return nil
}

func (d *StreamD) LLMGenerate(
	ctx context.Context,
	prompt string,
) (string, error) {
	return d.llm.Generate(ctx, prompt)
}

func (d *StreamD) updateLLMConfig(
	ctx context.Context,
) error {
	return d.llm.UpdateConfig(ctx)
}

func (l *llm) UpdateConfig(
	ctx context.Context,
) error {
	return xsync.DoA1R1(ctx, &l.locker, l.updateConfigNoLock, ctx)
}

func (l *llm) updateConfigNoLock(
	ctx context.Context,
) error {
	if l.cancelFunc != nil {
		l.cancelFunc()
		l.cancelFunc = nil
	}

	d := l.streamD
	cfg, err := d.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to get config: %w", err)
	}

	if l.backend != nil {
		l.backend.Close()
		l.backend = nil
	}

	endpoint := cfg.LLM.Endpoints["ChatGPT"]
	if endpoint == nil {
		return nil
	}

	backend, err := llms.NewChatGPT(xcontext.DetachDone(ctx), endpoint.ModelName, secret.New(endpoint.APIKey))
	if err != nil {
		return fmt.Errorf("unable to initialize ")
	}

	l.backend = backend
	return nil
}

func (l *llm) Generate(
	ctx context.Context,
	prompt string,
) (_ret string, _err error) {
	logger.Debugf(ctx, "Generate(ctx, '%s')", prompt)
	defer func() { logger.Debugf(ctx, "/Generate(ctx, '%s'): '%s', %v", prompt, _ret, _err) }()
	return xsync.DoA2R2(ctx, &l.locker, l.generateNoLock, ctx, prompt)
}

func (l *llm) generateNoLock(
	ctx context.Context,
	prompt string,
) (string, error) {
	if l.backend == nil {
		return "", fmt.Errorf("no LLM initialized")
	}

	return l.backend.Generate(ctx, prompt)
}
