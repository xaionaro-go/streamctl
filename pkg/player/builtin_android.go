//go:build android
// +build android

package player

import (
	"context"
	"fmt"
)

const SupportedBuiltin = false

func NewBuiltin(
	ctx context.Context,
	title string,
) (Player, error) {
	return nil, fmt.Errorf("not supported, yet")
}

func (m *Manager) NewBuiltin(
	ctx context.Context,
	title string,
) (Player, error) {
	return NewBuiltin(ctx, title)
}
