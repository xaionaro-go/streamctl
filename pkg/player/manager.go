package player

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type Manager struct {
	Config types.Config

	PlayersLocker xsync.Mutex
	Players       []Player
}

func NewManager(opts ...types.Option) *Manager {
	return &Manager{
		Config: types.Options(opts).Config(),
	}
}

type Backend string

const (
	BackendUndefined = ""
	BackendLibVLC    = "libvlc"
	BackendMPV       = "mpv"
)

func SupportedBackends() []Backend {
	var result []Backend
	if SupportedLibVLC {
		result = append(result, BackendLibVLC)
	}
	if SupportedMPV {
		result = append(result, BackendMPV)
	}
	return result
}

func (m *Manager) SupportedBackends() []Backend {
	return SupportedBackends()
}

func (m *Manager) NewPlayer(
	ctx context.Context,
	title string,
	backend Backend,
) (Player, error) {
	switch backend {
	case BackendLibVLC:
		return m.NewLibVLC(ctx, title)
	case BackendMPV:
		return m.NewMPV(ctx, title)
	default:
		return nil, fmt.Errorf("unexpected backend type: '%s'", backend)
	}
}
