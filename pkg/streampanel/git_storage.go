package streampanel

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
)

func (p *Panel) InputGitUserData(
	ctx context.Context,
) (bool, string, []byte, error) {
	logger.Debugf(ctx, "InputGitUserData")
	defer logger.Debugf(ctx, "/InputGitUserData")

	w := p.app.NewWindow("Configure GIT")
	w.Resize(fyne.NewSize(600, 600))

	waitCh := make(chan struct{})
	skip := false
	skipButton := widget.NewButtonWithIcon("Skip", theme.ConfirmIcon(), func() {
		skip = true
		close(waitCh)
	})
	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	cfg, err := p.StreamD.GetConfig(ctx)
	if err != nil {
		return false, "", nil, fmt.Errorf("unable to get the config from StreamD: %w", err)
	}

	gitRepo := widget.NewEntry()
	gitRepo.SetText(cfg.GitRepo.URL)
	gitRepo.SetPlaceHolder("git@github.com:myname/myrepo.git")

	gitPrivateKey := widget.NewMultiLineEntry()
	gitPrivateKey.SetText(string(cfg.GitRepo.PrivateKey))
	gitPrivateKey.SetMinRowsVisible(10)
	gitPrivateKey.TextStyle.Monospace = true
	gitPrivateKey.SetPlaceHolder(`-----BEGIN OPENSSH PRIVATE KEY-----
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX=
-----END OPENSSH PRIVATE KEY-----`)

	gitInstruction := widget.NewRichTextFromMarkdown("We can sync the configuration among all of your devices via a git repository. To get a git repository you may, for example, use GitHub; but never use public repositories, always use private ones (because the repository will contain all the access credentials to YouTube/Twitch/whatever).")
	gitInstruction.Wrapping = fyne.TextWrapWord

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(
			skipButton, okButton,
		),
		nil,
		nil,
		container.NewVBox(
			gitInstruction,
			widget.NewLabel("Remote repository:"),
			gitRepo,
			widget.NewLabel("Private key:"),
			gitPrivateKey,
		),
	))

	w.Show()
	<-waitCh
	w.Hide()

	if skip {
		return false, "", nil, nil
	}

	url := strings.Trim(gitRepo.Text, " \t\r\n")
	privateKey := strings.Trim(gitPrivateKey.Text, " \t\r\n")

	return true, url, []byte(privateKey), nil
}

func (p *Panel) Restart(
	_ context.Context,
	msg string,
) {
	w := p.app.NewWindow("Needs restart: " + msg)
	w.Resize(fyne.NewSize(400, 300))
	textWidget := widget.NewMultiLineEntry()
	textWidget.SetText(msg)
	textWidget.Wrapping = fyne.TextWrapWord
	textWidget.TextStyle = fyne.TextStyle{
		Bold:      true,
		Monospace: true,
	}
	w.SetContent(textWidget)
	w.Show()
}
