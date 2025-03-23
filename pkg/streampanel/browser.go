package streampanel

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	gconsts "github.com/xaionaro-go/streamctl/pkg/consts"
	"github.com/xaionaro-go/xsync"
)

type browser struct {
	*Panel

	locker         xsync.Mutex
	lastOpenedLink string
}

func newBrowser(p *Panel) *browser {
	return &browser{
		Panel: p,
	}
}

func (b *browser) openBrowser(
	ctx context.Context,
	urlString string,
	reason string,
) (_err error) {
	logger.Debugf(ctx, "openBrowser(ctx, '%s', '%s')", urlString, reason)
	defer func() { logger.Debugf(ctx, "/openBrowser(ctx, '%s', '%s'): %v", urlString, reason, _err) }()

	return xsync.DoA3R1(ctx, &b.locker, b.openBrowserNoLock, ctx, urlString, reason)
}

func (b *browser) openBrowserNoLock(
	ctx context.Context,
	urlString string,
	reason string,
) (_err error) {
	if urlString == b.lastOpenedLink {
		logger.Debugf(ctx, "the link '%s' was already opened, skipping", urlString)
		return nil
	}
	logger.Debugf(ctx, "the previous link was '%s', so the provided link '%s' is a new one (not a duplicate to be ignored)", b.lastOpenedLink, urlString)
	b.lastOpenedLink = urlString

	if b.Config.Browser.Command != "" {
		args := []string{b.Config.Browser.Command, urlString}
		logger.Debugf(
			ctx,
			"the browser command is configured to be '%s', so running '%s'",
			b.Config.Browser.Command,
			strings.Join(args, " "),
		)
		return exec.Command(args[0], args[1:]...).Start()
	}

	var browserCmd string
	switch runtime.GOOS {
	case "linux":
		if envBrowser := os.Getenv("BROWSER"); envBrowser != "" {
			browserCmd = envBrowser
		} else {
			browserCmd = "xdg-open"
		}
	default:
		url, err := url.Parse(urlString)
		if err != nil {
			return fmt.Errorf("unable to parse URL '%s': %w", urlString, err)
		}
		return b.app.OpenURL(url)
	}

	waitCh := make(chan struct{})

	w := b.app.NewWindow(gconsts.AppName + ": Browser selection window")
	resizeWindow(w, fyne.NewSize(600, 400))
	if reason != "" {
		reason += ". "
	}
	promptText := widget.NewRichTextWithText(reason + "Select a browser for that:")
	promptText.Wrapping = fyne.TextWrapWord
	browserField := widget.NewEntry()
	browserField.SetText(browserCmd)
	browserField.PlaceHolder = "command to execute the browser"
	browserField.OnSubmitted = func(s string) {
		close(waitCh)
	}
	okButton := widget.NewButton("OK", func() {
		close(waitCh)
	})
	w.SetContent(container.NewBorder(
		container.NewVBox(
			promptText,
			browserField,
		),
		okButton,
		nil,
		nil,
		nil,
	))

	w.Show()
	<-waitCh
	w.Hide()

	browserCmd = browserField.Text
	logger.Debugf(ctx, "chosen browser command is: '%s'", browserCmd)
	if browserCmd != b.Config.Browser.Command {
		logger.Debugf(ctx, "updating the browser command in the config")
		b.Config.Browser.Command = browserCmd
		err := b.SaveConfig(ctx)
		errmon.ObserveErrorCtx(ctx, err)
	}

	logger.Debugf(ctx, "openBrowser(ctx, '%s', '%s'): resulting command '%s %s'", urlString, reason, browserCmd, urlString)
	return exec.Command(browserCmd, urlString).Start()
}
