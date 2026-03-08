package config

import (
	"context"
	"fmt"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	streamserver "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

type ProfileMetadata struct {
	DefaultStreamTitle       string
	DefaultStreamDescription string
	MaxOrder                 int
}

type config struct {
	CachePath           *string `yaml:"cache_path"`
	ChatMessagesStorage *string `yaml:"chat_messages_storage"`
	GitRepo             GitRepoConfig
	Backends            streamcontrol.Config
	SelectedStreamIDs   []streamcontrol.StreamIDFullyQualified `yaml:"selected_stream_ids,omitempty"`
	ProfileMetadata     map[streamcontrol.ProfileName]ProfileMetadata
	StreamServer        streamserver.Config `yaml:"stream_server"`
	Dashboard           DashboardConfig     `yaml:"monitor"` // TODO: rename to `dashboard`
	TriggerRules        TriggerRules        `yaml:"trigger_rules"`
	P2PNetwork          P2PNetwork          `yaml:"p2p_network"`
	LLM                 LLM                 `yaml:"llm"`
	Shoutout            Shoutout            `yaml:"shoutout"`
	Raid                Raid                `yaml:"raid"`
}

type Config config

func NewConfig() Config {
	cfg := streamcontrol.Config{}
	obs.InitConfig(cfg)
	twitch.InitConfig(cfg)
	kick.InitConfig(cfg)
	youtube.InitConfig(cfg)
	return Config{
		Backends:        cfg,
		ProfileMetadata: map[streamcontrol.ProfileName]ProfileMetadata{},
		CachePath:       ptr("~/.streamd.cache"),
		Dashboard: DashboardConfig{
			Elements: map[string]DashboardElementConfig{},
		},
		P2PNetwork: GetRandomP2PConfig(),
	}
}

func NewSampleConfig() Config {
	cfg := NewConfig()
	cfg.Backends[obs.ID].Accounts = map[streamcontrol.AccountID]streamcontrol.RawMessage{
		streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[obs.StreamProfile]{
			StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[obs.StreamProfile]{
				streamcontrol.DefaultStreamID: {
					"some_profile": {},
				},
			},
		}),
	}
	cfg.Backends[twitch.ID].Accounts = map[streamcontrol.AccountID]streamcontrol.RawMessage{
		streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[twitch.StreamProfile]{
			StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[twitch.StreamProfile]{
				streamcontrol.DefaultStreamID: {
					"some_profile": {},
				},
			},
		}),
	}
	cfg.Backends[kick.ID].Accounts = map[streamcontrol.AccountID]streamcontrol.RawMessage{
		streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[kick.StreamProfile]{
			StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[kick.StreamProfile]{
				streamcontrol.DefaultStreamID: {
					"some_profile": {},
				},
			},
		}),
	}
	cfg.Backends[youtube.ID].Accounts = map[streamcontrol.AccountID]streamcontrol.RawMessage{
		streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[youtube.StreamProfile]{
			StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[youtube.StreamProfile]{
				streamcontrol.DefaultStreamID: {
					"some_profile": {},
				},
			},
		}),
	}
	return cfg
}

func (cfg *Config) Convert() error {
	return nil
}

var _ = NewSampleConfig

func ReadConfigFromPath(
	ctx context.Context,
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

func ReadOrCreateConfigFile(
	ctx context.Context,
	dataPath string,
) (*Config, error) {
	_, err := os.Stat(dataPath)
	switch {
	case err == nil:
		data := Config{}
		err := ReadConfigFromPath(ctx, dataPath, &data)
		if err != nil {
			return nil, fmt.Errorf("unable to read panel data from path '%s': %w", dataPath, err)
		}
		return &data, nil
	case os.IsNotExist(err):
		logger.Debugf(ctx, "cannot find file '%s', creating", dataPath)
		data := NewConfig()
		err := WriteConfigToPath(ctx, dataPath, data)
		if err != nil {
			logger.Errorf(ctx, "unable to write config to path '%s': %v", dataPath, err)
		}
		return &data, nil
	default:
		return nil, fmt.Errorf("unable to access file '%s': %w", dataPath, err)
	}
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
	logger.Infof(ctx, "wrote to '%s' the streamd config %#+v", cfgPath, cfg)
	return nil
}
