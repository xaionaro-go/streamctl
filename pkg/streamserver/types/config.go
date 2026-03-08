package types

import (
	"time"

	player "github.com/xaionaro-go/player/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Config struct {
	PortServers []streamportserver.Config          `yaml:"servers"`
	Streams     map[StreamSourceID]*StreamConfig   `yaml:"streams"`
	StaticSinks map[StreamSinkID]*StreamSinkConfig `yaml:"static_sinks"`
	VideoPlayer struct {
		MPV struct {
			Path string `yaml:"path"`
		} `yaml:"mpv"`
	} `yaml:"video_player"`
}

type ForwardingConfig struct {
	Disabled bool             `yaml:"disabled,omitempty"`
	Encode   EncodeConfig     `yaml:"encode,omitempty"`
	Quirks   ForwardingQuirks `yaml:"quirks,omitempty"`
}

type PlayerConfig struct {
	Player         player.Backend `yaml:"player,omitempty"`
	Disabled       bool           `yaml:"disabled,omitempty"`
	StreamPlayback sptypes.Config `yaml:"stream_playback,omitempty"`
}

type StreamConfig struct {
	Forwardings map[StreamSinkIDFullyQualified]ForwardingConfig `yaml:"forwardings"`
	Player      *PlayerConfig                                   `yaml:"player,omitempty"`
}

type StreamSinkConfig struct {
	URL            string                                `yaml:"url"`
	StreamKey      secret.String                         `yaml:"stream_key"`
	StreamSourceID *streamcontrol.StreamIDFullyQualified `yaml:"stream_source_id,omitempty"`
}

type RestartUntilPlatformRecognizesStream struct {
	Enabled        bool          `yaml:"enabled,omitempty"`
	StartTimeout   time.Duration `yaml:"start_timeout,omitempty"`
	StopStartDelay time.Duration `yaml:"stop_start_delay,omitempty"`
}

func DefaultRestartUntilPlatformRecognizesStreamConfig() RestartUntilPlatformRecognizesStream {
	return RestartUntilPlatformRecognizesStream{
		Enabled:        false,
		StartTimeout:   20 * time.Second,
		StopStartDelay: 10 * time.Second,
	}
}

type WaitUntilPlatformRecognizesStream struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

func DefaultWaitUntilPlatformRecognizesStreamConfig() WaitUntilPlatformRecognizesStream {
	return WaitUntilPlatformRecognizesStream{
		Enabled: false,
	}
}

type ForwardingQuirks struct {
	RestartUntilPlatformRecognizesStream RestartUntilPlatformRecognizesStream `yaml:"restart_until_platform_recognizes_stream,omitempty"`
	WaitUntilPlatformRecognizesStream    WaitUntilPlatformRecognizesStream    `yaml:"wait_until_platform_recognizes_stream"`
}

type EncodeConfig struct {
	Enabled                    bool `yaml:"enabled"`
	streamtypes.EncodersConfig `yaml:"config"`
}
