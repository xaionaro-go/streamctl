package config

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type Config struct {
	RemoteStreamDAddr string         `yaml:"streamd_remote"`
	BuiltinStreamD    streamd.Config `yaml:"streamd_builtin"`
}

func DefaultConfig() Config {
	return Config{
		BuiltinStreamD: streamd.NewSampleConfig(),
	}
}

func ReadConfigFromPath[CFG Config](
	cfgPath string,
	cfg *Config,
) error {
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	_, err = cfg.Read(b)
	return err
}

func WriteConfigToPath(
	ctx context.Context,
	cfgPath string,
	cfg Config,
) error {
	pathNew := cfgPath + ".new"
	f, err := os.OpenFile(pathNew, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0750)
	if err != nil {
		return fmt.Errorf("unable to open the data file '%s': %w", pathNew, err)
	}
	_, err = cfg.WriteTo(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to write data to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' config %#+v", cfgPath, cfg)
	return nil
}
