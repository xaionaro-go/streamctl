package player

import (
	"fmt"
	"sync"

	"github.com/xaionaro-go/streamctl/pkg/player/types"
)

type Manager struct {
	Config types.Config

	PlayersLocker sync.Mutex
	Players       []Player
}

func NewManager(opts ...types.Option) *Manager {
	return &Manager{}
}

func (m *Manager) NewMPV(title string) (*MPV, error) {
	r, err := NewMPV(title, m.Config.PathToMPV)
	if err != nil {
		return nil, err
	}

	m.PlayersLocker.Lock()
	defer m.PlayersLocker.Unlock()
	m.Players = append(m.Players, r)
	return r, nil
}

type Backend string

const (
	BackendUndefined = ""
	BackendVLC       = "vlc"
	BackendMPV       = "mpv"
)

func SupportedBackends() []Backend {
	var result []Backend
	if SupportedVLC {
		result = append(result, BackendVLC)
	}
	result = append(result, BackendMPV)
	return result
}

func (m *Manager) SupportedBackends() []Backend {
	return SupportedBackends()
}

func (m *Manager) NewPlayer(
	title string,
	backend Backend,
) (Player, error) {
	switch backend {
	case BackendVLC:
		return m.NewVLC(title)
	case BackendMPV:
		return m.NewMPV(title)
	default:
		return nil, fmt.Errorf("unexpected backend type: '%s'", backend)
	}
}
