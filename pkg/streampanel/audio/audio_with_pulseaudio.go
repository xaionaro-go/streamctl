//go:build disable_pulseaudio
// +build disable_pulseaudio

package audio

import (
	_ "github.com/xaionaro-go/audio/pkg/audio/backends/pulseaudio"
)
