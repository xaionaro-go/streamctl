package streamd

import (
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func convertConfig(cfg config.Config) error {
	return cfg.Convert()
}
