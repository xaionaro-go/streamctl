package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"unicode"

	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/player/types"
)

func backendsToStrings(backends []player.Backend) []string {
	result := make([]string, 0, len(backends))
	for _, s := range backends {
		result = append(result, string(s))
	}
	return result
}

func assertNoError(err error) {
	if err == nil {
		return
	}
	log.Fatal(err)
}

func main() {
	backends := backendsToStrings(player.SupportedBackends())

	mpvPath := pflag.String("mpv", "mpv", "path to mpv")
	backend := pflag.String("backend", backends[0], "player backend, supported values: "+strings.Join(backends, ", "))
	pflag.Parse()
	if pflag.NArg() != 1 {
		log.Fatal("exactly one argument expected")
	}
	mediaPath := pflag.Arg(0)

	m := player.NewManager(types.OptionPathToMPV(*mpvPath))
	p, err := m.NewPlayer("player demonstration", player.Backend(*backend))
	assertNoError(err)

	assertNoError(p.OpenURL(mediaPath))

	app := fyneapp.New()

	go func() {
		for {
			<-p.EndChan()
			w := app.NewWindow("file ended")
			b := widget.NewButton("Close", func() {
				w.Close()
			})
			w.SetContent(container.NewStack(b))
			w.Show()
		}
	}()

	errorMessage := widget.NewLabel("")

	setSpeed := widget.NewEntry()
	setSpeed.SetText("1")
	setSpeed.OnChanged = func(s string) {
		filtered := removeNonDigits(s)
		if s != filtered {
			setSpeed.SetText(filtered)
		}
	}
	setSpeed.OnSubmitted = func(s string) {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to parse speed '%s': %s", s, err))
			return
		}

		err = p.SetSpeed(f)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to set speed to '%f': %s", f, err))
			return
		}
		errorMessage.SetText("")
	}

	isPaused := false
	p.SetPause(isPaused)
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
		err := p.SetPause(isPaused)
		if err != nil {
			errorMessage.SetText(fmt.Sprintf("unable to set pause to '%v': %s", isPaused, err))
			return
		}
		errorMessage.SetText("")
	})

	stopButton := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		p.Stop()
	})

	closeButton := widget.NewButtonWithIcon("Close", theme.WindowCloseIcon(), func() {
		p.Stop()
	})

	w := app.NewWindow("player controls")
	w.SetContent(container.NewBorder(
		nil,
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

func removeNonDigits(input string) string {
	var result []rune
	for _, r := range input {
		if unicode.IsDigit(r) {
			result = append(result, r)
		}
	}
	return string(result)
}
