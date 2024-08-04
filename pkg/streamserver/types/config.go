package types

import (
	"time"

	"github.com/xaionaro-go/streamctl/pkg/player"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
)

type Server struct {
	Type   streamtypes.ServerType `yaml:"protocol"`
	Listen string                 `yaml:"listen"`
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

type ForwardingQuirks struct {
	RestartUntilYoutubeRecognizesStream RestartUntilYoutubeRecognizesStream `yaml:"restart_until_youtube_recognizes_stream,omitempty"`
}

type ForwardingConfig struct {
	Disabled bool             `yaml:"disabled,omitempty"`
	Quirks   ForwardingQuirks `yaml:"quirks,omitempty"`
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
	URL string `yaml:"url"`
}

type Config struct {
	Servers      []Server                             `yaml:"servers"`
	Streams      map[StreamID]*StreamConfig           `yaml:"streams"`
	Destinations map[DestinationID]*DestinationConfig `yaml:"destinations"`
	VideoPlayer  struct {
		MPV struct {
			Path string `yaml:"path"`
		} `yaml:"mpv"`
	} `yaml:"video_player"`
}
