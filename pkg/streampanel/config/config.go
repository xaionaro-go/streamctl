package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"fyne.io/fyne/v2"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/screenshot"
	streamd "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type ScreenshotConfig struct {
	Enabled           *bool `yaml:"enabled"`
	screenshot.Config `yaml:"screenshot,inline"`
}

type BrowserConfig struct {
	Command string `yaml:"command"`
}

type OAuthConfig struct {
	ListenPorts struct {
		Twitch  uint16 `yaml:"twitch"`
		Kick    uint16 `yaml:"kick"`
		YouTube uint16 `yaml:"youtube"`
	} `yaml:"listen_ports"`
}

type ChatConfig struct {
	CommandOnReceiveMessage        string `yaml:"command_on_receive_message,omitempty"`
	EnableNotifications            *bool  `yaml:"enable_notifications,omitempty"`
	EnableReceiveMessageSoundAlarm *bool  `yaml:"enable_receive_message_sound_alarm,omitempty"`
}

func (cfg ChatConfig) NotificationsEnabled() bool {
	return cfg.EnableNotifications == nil || *cfg.EnableNotifications
}

func (cfg ChatConfig) ReceiveMessageSoundAlarmEnabled() bool {
	return cfg.EnableReceiveMessageSoundAlarm == nil || *cfg.EnableReceiveMessageSoundAlarm
}

type DashboardConfig struct {
	Size struct {
		Width  uint `yaml:"width"`
		Height uint `yaml:"height"`
	} `yaml:"size"`
}

type StreamMonitor struct {
	IsEnabled      bool                       `yaml:"is_enabled"`
	StreamDAddr    string                     `yaml:"streamd_addr"`
	StreamSourceID streamtypes.StreamSourceID `yaml:"stream_source_id"`
	VideoTracks    []uint                     `yaml:"tracks_video,omitempty"`
	AudioTracks    []uint                     `yaml:"tracks_audio,omitempty"`
}

type MonitorsConfig struct {
	StreamMonitors []StreamMonitor `yaml:"streams"`
}

type Config struct {
	App               fyne.App         `yaml:"-"`
	RemoteStreamDAddr string           `yaml:"streamd_remote"`
	BuiltinStreamD    streamd.Config   `yaml:"streamd_builtin"`
	Screenshot        ScreenshotConfig `yaml:"screenshot"`
	Browser           BrowserConfig    `yaml:"browser"`
	OAuth             OAuthConfig      `yaml:"oauth"`
	Chat              ChatConfig       `yaml:"chat"`
	Dashboard         DashboardConfig  `yaml:"dashboard"`
	Monitors          MonitorsConfig   `yaml:"monitors"`
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
) (_err error) {
	logger.Debugf(ctx, "WriteConfigToPath")
	defer func() { logger.Debugf(ctx, "/WriteConfigToPath: %v", _err) }()

	pathNew := cfgPath + ".new"
	f, err := os.OpenFile(pathNew, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0750)
	if err != nil {
		return fmt.Errorf("unable to open the data file '%s': %w", pathNew, err)
	}

	logger.Tracef(ctx, "cfg.WriteTo: %s", spew.Sdump(cfg))
	_, err = cfg.WriteTo(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to write data to file '%s': %w", pathNew, err)
	}

	backupDir := fmt.Sprintf("%s-backup", cfgPath)
	err = os.MkdirAll(backupDir, 0755)
	if err != nil {
		logger.Errorf(ctx, "unable to create directory '%s'", backupDir)
	} else {
		now := time.Now()
		pathBackup := path.Join(
			backupDir,
			fmt.Sprintf(
				"%04d%02d%02d_%02d%02d.yaml",
				now.Year(), now.Month(), now.Day(),
				now.Hour(), now.Minute(),
			),
		)

		logger.Debugf(ctx, "backup path: '%s'", pathBackup)
		err = os.Rename(cfgPath, pathBackup)
		if err != nil {
			logger.Errorf(ctx, "cannot move '%s' to '%s': %w", cfgPath, pathBackup, err)
		}
	}

	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' the streampanel config %#+v", cfgPath, cfg)

	return nil
}
