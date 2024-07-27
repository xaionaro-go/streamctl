package player

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/player/types"
)

type Manager struct {
	Config types.Config
}

func NewManager(opts ...types.Option) *Manager {
	return &Manager{}
}

func (m *Manager) NewMPV(title string) (*MPV, error) {
	return NewMPV(title, m.Config.PathToMPV)
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
