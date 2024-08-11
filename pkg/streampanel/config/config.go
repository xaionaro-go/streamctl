package config

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type ScreenshotConfig struct {
	Enabled           *bool `yaml:"enabled"`
	screenshot.Config `yaml:"screenshot,inline"`
}

type BrowserConfig struct {
	Command string `yaml:"command"`
}

type Config struct {
	RemoteStreamDAddr string           `yaml:"streamd_remote"`
	BuiltinStreamD    streamd.Config   `yaml:"streamd_builtin"`
	Screenshot        ScreenshotConfig `yaml:"screenshot"`
	Browser           BrowserConfig    `yaml:"browser"`
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
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		return nil
	}

	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	logger.Default().Tracef("unparsed config == %s", b)
	_, err = cfg.Read(b)

	var cfgSerialized bytes.Buffer
	if _, _err := cfg.WriteTo(&cfgSerialized); _err != nil {
		logger.Default().Error(_err)
	} else {
		logger.Default().Tracef("parsed config == %s", cfgSerialized.String())
	}

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
