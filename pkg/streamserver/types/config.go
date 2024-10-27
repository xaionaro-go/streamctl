package types

import (
	"time"

	player "github.com/xaionaro-go/streamctl/pkg/player/types"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Config struct {
	PortServers  []streamportserver.Config            `yaml:"servers"`
	Streams      map[StreamID]*StreamConfig           `yaml:"streams"`
	Destinations map[DestinationID]*DestinationConfig `yaml:"destinations"`
	VideoPlayer  struct {
		MPV struct {
			Path string `yaml:"path"`
		} `yaml:"mpv"`
	} `yaml:"video_player"`
}

type ForwardingConfig struct {
	Disabled bool               `yaml:"disabled,omitempty"`
	Quirks   ForwardingQuirks   `yaml:"quirks,omitempty"`
	Convert  VideoConvertConfig `yaml:"convert,omitempty"`
}

type PlayerConfig struct {
	Player         player.Backend `yaml:"player,omitempty"`
	Disabled       bool           `yaml:"disabled,omitempty"`
	StreamPlayback sptypes.Config `yaml:"stream_playback,omitempty"`
}

type StreamConfig struct {
	Forwardings map[DestinationID]ForwardingConfig `yaml:"forwardings"`
	Player      *PlayerConfig                      `yaml:"player,omitempty"`
}

type DestinationConfig struct {
	URL       string `yaml:"url"`
	StreamKey string `yaml:"stream_key"`
}

type RestartUntilYoutubeRecognizesStream struct {
	Enabled        bool          `yaml:"enabled,omitempty"`
	StartTimeout   time.Duration `yaml:"start_timeout,omitempty"`
	StopStartDelay time.Duration `yaml:"stop_start_delay,omitempty"`
}

func DefaultRestartUntilYoutubeRecognizesStreamConfig() RestartUntilYoutubeRecognizesStream {
	return RestartUntilYoutubeRecognizesStream{
		Enabled:        false,
		StartTimeout:   20 * time.Second,
		StopStartDelay: 10 * time.Second,
	}
}

type StartAfterYoutubeRecognizedStream struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

func DefaultStartAfterYoutubeRecognizedStreamConfig() StartAfterYoutubeRecognizedStream {
	return StartAfterYoutubeRecognizedStream{
		Enabled: false,
	}
}

type ForwardingQuirks struct {
	RestartUntilYoutubeRecognizesStream RestartUntilYoutubeRecognizesStream `yaml:"restart_until_youtube_recognizes_stream,omitempty"`
	StartAfterYoutubeRecognizedStream   StartAfterYoutubeRecognizedStream   `yaml:"start_after_youtube_recognizes_stream"`
}

type VideoConvertConfig = streamtypes.VideoConvertConfig
