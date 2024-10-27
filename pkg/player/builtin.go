package player

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/player/builtin"
)

const SupportedBuiltin = false

type Builtin = builtin.Player

func NewBuiltin(
	ctx context.Context,
	title string,
) (*Builtin, error) {
	return builtin.New(ctx, title), nil
}

func (m *Manager) NewBuiltin(
	ctx context.Context,
	title string,
) (*Builtin, error) {
	return NewBuiltin(ctx, title)
}
