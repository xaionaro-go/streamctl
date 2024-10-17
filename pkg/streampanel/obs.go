package streampanel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func (p *Panel) setStreamDConfig(
	ctx context.Context,
	cfg *config.Config,
) error {
	if err := p.StreamD.SetConfig(ctx, cfg); err != nil {
		return fmt.Errorf("unable to set the config: %w", err)
	}
	if err := p.StreamD.SaveConfig(ctx); err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}
	return nil
}
