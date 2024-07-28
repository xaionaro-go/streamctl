package main

import (
	"context"
	"log"
	"strings"

	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"

	"github.com/xaionaro-go/streamctl/pkg/player"
	ptypes "github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
)

func assertNoError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	logger.Fatal(ctx, err)
}

func main() {
	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	rtmpListenAddr := pflag.String("rtmp-listen-addr", "127.0.0.1:1935", "the TCP port to serve an RTMP server on")
	streamID := pflag.String("stream-id", "test/test", "the path of the stream in rtmp://address/path")
	mpvPath := pflag.String("mpv", "mpv", "path to mpv")
	pflag.Parse()

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if pflag.NArg() != 0 {
		log.Fatal("exactly zero arguments expected")
	}

	m := player.NewManager(ptypes.OptionPathToMPV(*mpvPath))
	ss := streamserver.New(&sstypes.Config{
		Servers: []sstypes.Server{{
			Type:   sstypes.ServerTypeRTMP,
			Listen: *rtmpListenAddr,
		}},
		Streams: map[sstypes.StreamID]*sstypes.StreamConfig{
			sstypes.StreamID(*streamID): {},
		},
	})
	err := ss.Init(ctx)
	assertNoError(ctx, err)
	sp := streamplayer.New(NewStreamPlayerStreamServer(ss), m)
	_, err = sp.Create(ctx, api.StreamID(*streamID))
	assertNoError(ctx, err)

	app := fyneapp.New()

	errorMessage := widget.NewLabel("")

	closeButton := widget.NewButtonWithIcon("Close", theme.WindowCloseIcon(), func() {
		err := sp.Remove(ctx, api.StreamID(*streamID))
		assertNoError(ctx, err)
	})

	w := app.NewWindow("player controls")
	w.SetContent(container.NewBorder(
		nil,
		errorMessage,
		nil,
		nil,
		container.NewVBox(
			closeButton,
		),
	))
	w.Show()
	app.Run()
}

type StreamPlayerStreamServer struct {
	StreamServer *streamserver.StreamServer
}

var _ streamplayer.StreamServer = (*StreamPlayerStreamServer)(nil)

func NewStreamPlayerStreamServer(ss *streamserver.StreamServer) *StreamPlayerStreamServer {
	return &StreamPlayerStreamServer{
		StreamServer: ss,
	}
}

func (s *StreamPlayerStreamServer) GetPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	result := make([]streamplayer.StreamPortServer, 0, len(s.StreamServer.ServerHandlers))
	for _, srv := range s.StreamServer.ServerHandlers {
		result = append(result, streamplayer.StreamPortServer{
			Addr: srv.ListenAddr(),
			Type: srv.Type(),
		})
	}
	return result, nil
}

func (s *StreamPlayerStreamServer) WaitPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	streamIDParts := strings.Split(string(streamID), "/")
	localAppName := string(streamID)
	if len(streamIDParts) == 2 {
		localAppName = streamIDParts[1]
	}

	ch := make(chan struct{})
	go func() {
		s.StreamServer.RelayServer.WaitPubsub(ctx, localAppName)
		close(ch)
	}()
	return ch, nil
}
