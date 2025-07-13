package action

import (
	"github.com/xaionaro-go/serializable"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func init() {
	serializable.RegisterType[StartStreamByProfileName]()
}

type StartStreamByProfileName struct {
	PlatID      streamcontrol.PlatformName `yaml:"platform,omitempty"     json:"platform,omitempty"`
	Title       *string                    `yaml:"title,omitempty"        json:"title,omitempty"`
	Description *string                    `yaml:"description,omitempty"  json:"description,omitempty"`
	ProfileName string                     `yaml:"profile_name,omitempty" json:"profile_name,omitempty"`
}

var _ Action = (*StartStreamByProfileName)(nil)

func (*StartStreamByProfileName) isAction() {}

func (a *StartStreamByProfileName) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}
