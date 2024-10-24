package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/player/types"
	"github.com/xaionaro-go/streamctl/pkg/xfyne"
)

func backendsToStrings(backends []player.Backend) []string {
	result := make([]string, 0, len(backends))
	for _, s := range backends {
		result = append(result, string(s))
	}
	return result
}

func assertNoError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	logger.Fatal(ctx, err)
}

func main() {
	backends := backendsToStrings(player.SupportedBackends())
	loggerLevel := logger.LevelInfo
	pflag.Var(&loggerLevel, "log-level", "Log level")
	mpvPath := pflag.String("mpv", "mpv", "path to mpv")
	backend := pflag.String(
		"backend",
		backends[0],
		"player backend, supported values: "+strings.Join(backends, ", "),
	)
	pflag.Parse()

	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if pflag.NArg() != 1 {
		l.Fatal("exactly one argument expected")
	}
	mediaPath := pflag.Arg(0)

	m := player.NewManager(types.OptionPathToMPV(*mpvPath))
	p, err := m.NewPlayer(ctx, "player demonstration", player.Backend(*backend))
	assertNoError(ctx, err)

	assertNoError(ctx, p.OpenURL(ctx, mediaPath))

	app := fyneapp.New()

	observability.Go(ctx, func() {
		for {
			ch, err := p.EndChan(ctx)
			if err != nil {
				panic(err)
			}
			<-ch
			w := app.NewWindow("file ended")
			b := widget.NewButton("Close", func() {
				w.Close()
			})
			w.SetContent(container.NewStack(b))
			w.Show()
		}
	})

	errorMessage := widget.NewLabel("")

	setSpeed := xfyne.NewNumericalEntry()
	setSpeed.SetText("1.0")
	setSpeed.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to parse speed '%s': %s", s, err))
			return
		}

		err = p.SetSpeed(ctx, f)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to set speed to '%f': %s", f, err))
			return
		}
		errorMessage.SetText("")
	}

	isPaused := false
	p.SetPause(ctx, isPaused)
	var pauseUnpause *widget.Button
	pauseUnpause = widget.NewButtonWithIcon("Pause", theme.MediaPauseIcon(), func() {
		isPaused = !isPaused
		switch isPaused {
		case true:
			pauseUnpause.SetText("Unpause")
			pauseUnpause.SetIcon(theme.MediaPlayIcon())
		case false:
			pauseUnpause.SetText("Pause")
			pauseUnpause.SetIcon(theme.MediaPauseIcon())
		}
		err := p.SetPause(ctx, isPaused)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to set pause to '%v': %s", isPaused, err))
			return
		}
		errorMessage.SetText("")
	})

	stopButton := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		p.Stop(ctx)
	})

	closeButton := widget.NewButtonWithIcon("Close", theme.WindowCloseIcon(), func() {
		p.Stop(ctx)
	})

	posLabel := widget.NewLabel("")
	observability.Go(ctx, func() {
		t := time.NewTicker(time.Millisecond * 100)
		for {
			<-t.C
			l, err := p.GetLength(ctx)
			if err != nil {
				posLabel.SetText(fmt.Sprintf("unable to get the length: %v", err))
				return
			}

			pos, err := p.GetPosition(ctx)
			if err != nil {
				posLabel.SetText(fmt.Sprintf("unable to get the position: %v", err))
				return
			}

			posLabel.SetText(pos.String() + " / " + l.String())
		}
	})

	w := app.NewWindow("player controls")
	w.SetContent(container.NewBorder(
		posLabel,
		errorMessage,
		nil,
		nil,
		container.NewVBox(
			setSpeed,
			pauseUnpause,
			stopButton,
			closeButton,
		),
	))
	w.Show()
	app.Run()
}
