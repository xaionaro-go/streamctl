package audio

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/jfreymuth/oggvorbis"
	"github.com/xaionaro-go/streamctl/pkg/audio/registry"
)

const BufferSize = 100 * time.Millisecond

type Audio struct {
	PlayerPCM
}

func NewAudio(playerPCM PlayerPCM) *Audio {
	return &Audio{
		PlayerPCM: playerPCM,
	}
}

var (
	lastSuccessfulFactory       registry.PlayerPCMFactory
	lastSuccessfulFactoryLocker sync.Mutex
)

func getLastSuccessfulFactory() registry.PlayerPCMFactory {
	lastSuccessfulFactoryLocker.Lock()
	defer lastSuccessfulFactoryLocker.Unlock()
	return lastSuccessfulFactory
}

func NewAudioAuto(
	ctx context.Context,
) *Audio {
	factory := getLastSuccessfulFactory()
	if factory != nil {
		player := factory.NewPlayerPCM()
		if err := player.Ping(); err == nil {
			return NewAudio(player)
		}
	}

	for _, factory := range registry.Factories() {
		player := factory.NewPlayerPCM()
		err := player.Ping()
		logger.Debugf(ctx, "pinging PCM player %T result is %v", player, err)
		if err == nil {
			lastSuccessfulFactoryLocker.Lock()
			defer lastSuccessfulFactoryLocker.Unlock()
			lastSuccessfulFactory = factory
			return NewAudio(player)
		}
	}

	logger.Infof(ctx, "was unable to initialize any PCM player")
	return &Audio{
		PlayerPCM: PlayerPCMDummy{},
	}
}

func (a *Audio) PlayVorbis(rawReader io.Reader) (Stream, error) {
	oggReader, err := oggvorbis.NewReader(rawReader)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize a vorbis reader: %w", err)
	}

	stream, err := a.PlayerPCM.PlayPCM(
		uint32(oggReader.SampleRate()),
		uint16(oggReader.Channels()),
		PCMFormatFloat32LE,
		BufferSize,
		newReaderFromFloat32Reader(oggReader),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to playback as PCM: %w", err)
	}
	return stream, nil
}

func (a *Audio) PlayPCM(
	sampleRate uint32,
	channels uint16,
	pcmFormat PCMFormat,
	bufferSize time.Duration,
	pcmReader io.Reader,
) (Stream, error) {
	return a.PlayerPCM.PlayPCM(
		sampleRate,
		channels,
		pcmFormat,
		bufferSize,
		pcmReader,
	)
}
