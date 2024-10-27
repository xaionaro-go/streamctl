package player

import (
	"github.com/xaionaro-go/streamctl/pkg/player/types"
)

type Player = types.Player
type PlayerCommon = types.PlayerCommon
type Backend = types.Backend

const (
	BackendUndefined = types.BackendUndefined
	BackendLibVLC    = types.BackendLibVLC
	BackendMPV       = types.BackendMPV
	BackendBuiltin   = types.BackendBuiltin
)
