//go:build with_libvlc
// +build with_libvlc

package player

import (
	"context"

	"github.com/xaionaro-go/streamctl/pkg/player/vlcserver"
)

const SupportedLibVLC = true

func (m *Manager) NewLibVLC(
	ctx context.Context,
	title string,
) (*LibVLC, error) {
	r, err := NewLibVLC(ctx, title)
	if err != nil {
		return nil, err
	}

	m.PlayersLocker.Lock()
	defer m.PlayersLocker.Unlock()
	m.Players = append(m.Players, r)
	return r, nil
}

type LibVLC = vlcserver.VLC

var _ Player = (*LibVLC)(nil)

func NewLibVLC(
	ctx context.Context,
	title string,
) (*LibVLC, error) {
	return vlcserver.Run(ctx, title)
}
