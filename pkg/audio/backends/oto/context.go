package oto

import (
	"fmt"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/xaionaro-go/streamctl/pkg/audio/types"
)

var (
	otoContext       *oto.Context
	otoContextLocker sync.Mutex
)

const (
	SampleRate = 48000
	Channels   = 2
	BufferSize = 100 * time.Millisecond
	Format     = types.PCMFormatFloat32LE
)

func getOtoContext() (*oto.Context, error) {
	otoContextLocker.Lock()
	defer otoContextLocker.Unlock()
	if otoContext != nil {
		return otoContext, nil
	}

	op := &oto.NewContextOptions{
		SampleRate:   SampleRate,
		ChannelCount: Channels,
		Format:       FormatToOto(Format),
		BufferSize:   BufferSize,
	}

	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return otoCtx, fmt.Errorf("unable to initialize an oto context: %w", err)
	}
	<-readyChan

	otoContext = otoCtx
	return otoContext, nil
}
