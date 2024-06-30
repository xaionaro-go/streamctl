package streampanel

import (
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/client"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
)

type StreamD = api.StreamD

func NewBuiltinStreamD(configPath string, ui ui.UI, b *belt.Belt) (*streamd.StreamD, error) {
	return streamd.New(configPath, ui, b)
}

func NewRemoteStreamD(target string, ui ui.UI, _ *belt.Belt) *client.Client {
	return client.New(target)
}
