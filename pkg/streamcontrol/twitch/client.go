package twitch

import (
	"github.com/nicklaw5/helix/v2"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
)

type client = twitch.Client

type clientNicklaw5 struct {
	*helix.Client
}
