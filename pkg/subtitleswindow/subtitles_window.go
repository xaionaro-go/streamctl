package subtitleswindow

import (
	"context"
	"fmt"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player/builtin"
)

type SubtitlesWindow struct {
	fyne.Window
	Container        *fyne.Container
	player           *builtin.Player
	speechRecognizer *speechRecognizer
	wg               sync.WaitGroup
	onceCloser       onceCloser
}

func New(
	ctx context.Context,
	app fyne.App,
	title string,
	medialURL string,
	whisperSrvAddr string,
) (_ret *SubtitlesWindow, _err error) {
	logger.Debugf(ctx, "New(ctx, app, '%s', '%s', '%s')", title, medialURL, whisperSrvAddr)
	defer func() {
		logger.Debugf(ctx, "/New(ctx, app, '%s', '%s', '%s'): %#+v %#+v", title, medialURL, whisperSrvAddr, _ret, _err)
	}()

	w := &SubtitlesWindow{}

	w.Container = container.NewStack()
	w.Window = app.NewWindow(title)
	w.Window.SetContent(container.NewVScroll(w.Container))
	w.Window.Resize(fyne.NewSize(1200, 600))

	var err error
	w.speechRecognizer, err = newSpeechRecognizer(ctx, whisperSrvAddr, w)
	logger.Debugf(ctx, "newSpeechRecognizer(): %#+v %#+v", w.speechRecognizer, err)
	if err != nil {
		w.Window.Close()
		return nil, fmt.Errorf("unable to initialize a new speech recognizer: %w", err)
	}

	w.player = builtin.New(ctx, nil, w.speechRecognizer)
	logger.Debugf(ctx, "builtin.New(ctx, nil, sr): %#+v", w.player)

	err = w.player.OpenURL(ctx, medialURL)
	logger.Debugf(ctx, "player.OpenURL(ctx, '%s'): %#+v", medialURL, err)
	if err != nil {
		if err := w.Close(); err != nil {
			logger.Errorf(ctx, "unable to close the subtitles window: %v", err)
		}
		return nil, fmt.Errorf("unable to open URL '%s' in the player: %w", medialURL, err)
	}

	return w, nil
}

func (w *SubtitlesWindow) Wait() error {
	w.wg.Wait()
	return nil
}

func (w *SubtitlesWindow) Close() error {
	var mErr *multierror.Error
	w.onceCloser.Do(func() {
		ctx := context.TODO()
		logger.Debugf(ctx, "Close")

		if err := w.player.Close(ctx); err != nil {
			mErr = multierror.Append(fmt.Errorf("unable to close the player: %v", err))
		}

		if err := w.speechRecognizer.Close(); err != nil {
			mErr = multierror.Append(fmt.Errorf("unable to close the speech recognizer: %v", err))
		}

		w.Window.Close()
	})
	return mErr.ErrorOrNil()
}
