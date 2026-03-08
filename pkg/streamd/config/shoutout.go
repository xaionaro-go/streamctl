package config

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatUserID struct {
	Platform streamcontrol.PlatformID `yaml:"platform"`
	User     streamcontrol.UserID     `yaml:"user"`
}

func (c ChatUserID) String() string {
	return string(c.Platform) + ":" + string(c.User)
}

type Shoutout struct {
	AutoShoutoutOnMessage []ChatUserID `yaml:"auto_shoutout_on_message"`
}
