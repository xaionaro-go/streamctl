package oto

import (
	"github.com/xaionaro-go/streamctl/pkg/audio/registry"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

const (
	Priority = 100
)

func init() {
	registry.RegisterFactory(Priority, PlayerPCMOtoFactory{})
}

type PlayerPCMOtoFactory struct{}

func (PlayerPCMOtoFactory) NewPlayerPCM() types.PlayerPCM {
	return NewPlayerPCM()
}
