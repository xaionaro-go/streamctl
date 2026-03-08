package streamcontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
)

type Config map[PlatformID]*AbstractPlatformConfig

var _ yaml.BytesUnmarshaler = (*Config)(nil)

func (cfg *Config) UnmarshalYAML(b []byte) error {
	t := map[PlatformID]*AbstractPlatformConfig{}
	err := yaml.Unmarshal(b, &t)
	if err != nil {
		return fmt.Errorf("unable to unmarshal YAML: %w", err)
	}
	*cfg = t
	return nil
}

func GetPlatformConfig[AC AccountConfigGeneric[SP], SP StreamProfile](
	ctx context.Context,
	cfg Config,
	id PlatformID,
) *PlatformConfig[AC, SP] {
	platCfg, ok := cfg[id]
	if !ok {
		logger.Debugf(ctx, "config '%s' was not found in cfg: %#+v", id, cfg)
		return nil
	}

	return ConvertPlatformConfig[AC, SP](ctx, platCfg)
}

func InitConfig[AC AccountConfigGeneric[SP], SP StreamProfile](cfg Config, id PlatformID, platCfg PlatformConfig[AC, SP]) {
	if _, ok := cfg[id]; ok {
		panic(fmt.Errorf("id '%s' is already registered", id))
	}
	cfg[id] = ToAbstractPlatformConfig(context.Background(), &platCfg)
}
