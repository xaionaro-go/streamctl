package config

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type ChatUserID struct {
	Platform streamcontrol.PlatformName `yaml:"platform"`
	User     streamcontrol.ChatUserID   `yaml:"user"`
}

type Shoutout struct {
	AutoShoutoutOnMessage []ChatUserID `yaml:"auto_shoutout_on_message"`
}
