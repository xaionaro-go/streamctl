package streampanel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/xaionaro-go/streamctl/pkg/repository"
)

const gitRepoPanelConfigFileName = "streampanel.yaml"
const gitCommitterName = "streampanel"
const gitCommitterEmail = "streampanel@noemail.invalid"

func (p *Panel) initGitIfNeeded(ctx context.Context) {
	logger.Debugf(ctx, "initGitIfNeeded")
	if p.data.GitRepo.Enable != nil && !*p.data.GitRepo.Enable {
		logger.Debugf(ctx, "git is disabled in the configuration")
		return
	}

	newUserData := false
	if p.data.GitRepo.URL == "" || p.data.GitRepo.PrivateKey == "" {
		newUserData = true
	}

	for {
		logger.Debugf(ctx, "attempting to configure a git storage")
		if newUserData {
			ok, err := p.inputGitUserData(ctx)
			if err != nil {
				p.displayError(fmt.Errorf("unable to input the git user data: %w", err))
				continue
			}
			if err := p.saveData(ctx); err != nil {
				p.displayError(err)
			}
			if !ok {
				p.deinitGitStorage(ctx)
				return
			}
		}

		logger.Debugf(ctx, "newGitStorage")
		gitStorage, err := repository.NewGit(
			ctx,
			p.data.GitRepo.URL,
			[]byte(p.data.GitRepo.PrivateKey),
			gitRepoPanelConfigFileName,
			gitCommitterName,
			gitCommitterEmail,
		)
		if err != nil {
			p.displayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
			if newUserData {
				continue
			}
			p.data.GitRepo.Enable = ptr(false)
			p.data.GitRepo.URL = ""
			p.data.GitRepo.PrivateKey = ""
			return
		}

		p.gitSyncerMutex.Lock()
		p.deinitGitStorage(ctx)
		if gitStorage == nil {
			panic("gitStorage == nil")
		}
		p.gitStorage = gitStorage
		p.gitSyncerMutex.Unlock()
		break
	}

	if p.gitStorage == nil {
		panic("p.gitStorage == nil")
	}
	p.startPeriodicGitSyncer(ctx)
	p.gitInitialized = true
}

func (p *Panel) deinitGitStorage(_ context.Context) {
	if p.gitStorage != nil {
		err := p.gitStorage.Close()
		if err != nil {
			p.displayError(err)
		}
		p.gitStorage = nil
	}
	if p.cancelGitSyncer != nil {
		p.cancelGitSyncer()
		p.cancelGitSyncer = nil
	}
}

func (p *Panel) startPeriodicGitSyncer(ctx context.Context) {
	logger.Debugf(ctx, "startPeriodicGitSyncer")
	defer logger.Debugf(ctx, "/startPeriodicGitSyncer")
	p.gitSyncerMutex.Lock()

	if p.cancelGitSyncer != nil {
		logger.Debugf(ctx, "git syncer is already started")
		p.gitSyncerMutex.Unlock()
		return
	}

	ctx, cancelFn := context.WithCancel(ctx)
	p.cancelGitSyncer = cancelFn
	p.gitSyncerMutex.Unlock()

	p.gitSync(ctx)
	go func() {
		err := p.sendConfigViaGIT(ctx)
		if err != nil {
			p.displayError(fmt.Errorf("unable to send the config to the remote git repository: %w", err))
		}

		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				p.gitSyncerMutex.Lock()
				defer p.gitSyncerMutex.Unlock()

				p.cancelGitSyncer = nil
				return
			case <-ticker.C:
			}

			p.gitSync(ctx)
		}
	}()
}

func (p *Panel) inputGitUserData(ctx context.Context) (bool, error) {
	logger.Debugf(ctx, "inputGitUserData")
	defer logger.Debugf(ctx, "/inputGitUserData")

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

	gitRepo := widget.NewEntry()
	gitRepo.SetText(p.data.GitRepo.URL)
	gitRepo.SetPlaceHolder("git@github.com:myname/myrepo.git")

	gitPrivateKey := widget.NewMultiLineEntry()
	gitPrivateKey.SetText(p.data.GitRepo.PrivateKey)
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
		p.data.GitRepo.Enable = ptr(false)
		return false, nil
	}

	p.data.GitRepo.Enable = ptr(true)
	p.data.GitRepo.URL = strings.Trim(gitRepo.Text, " \t\r\n")
	p.data.GitRepo.PrivateKey = strings.Trim(gitPrivateKey.Text, " \t\r\n")

	return true, nil
}

func (p *Panel) gitSync(ctx context.Context) {
	logger.Debugf(ctx, "gitSync")
	defer logger.Debugf(ctx, "/gitSync")

	p.gitSyncerMutex.Lock()
	defer p.gitSyncerMutex.Unlock()

	p.startStopMutex.Lock()

	if p.updateTimerHandler != nil {
		logger.Debugf(ctx, "skipping git sync: the stream is running")
		p.startStopMutex.Unlock()
		return
	}
	p.startStopMutex.Unlock()

	logger.Debugf(ctx, "last_known_commit: %s", p.data.GitRepo.LatestSyncCommit)
	err := p.gitStorage.Pull(
		ctx,
		p.getLastKnownGitCommitHash(),
		func(ctx context.Context, commitHash plumbing.Hash, b []byte) {
			panelData := newPanelData()
			err := readPanelData(ctx, b, &panelData)
			if err != nil {
				p.displayError(fmt.Errorf("unable to read panel data: %w", err))
				return
			}
			panelData.GitRepo.LatestSyncCommit = commitHash.String()
			p.onConfigUpdateViaGIT(ctx, &panelData)
		},
	)
	if err != nil {
		p.displayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
	}
}

func (p *Panel) setLastKnownGitCommitHash(newHash plumbing.Hash) {
	p.data.GitRepo.LatestSyncCommit = newHash.String()
}

func (p *Panel) getLastKnownGitCommitHash() plumbing.Hash {
	return plumbing.NewHash(p.data.GitRepo.LatestSyncCommit)
}

func (p *Panel) onConfigUpdateViaGIT(ctx context.Context, newData *panelData) {
	p.data = *newData
	err := p.saveData(ctx)
	if err != nil {
		p.displayError(fmt.Errorf("unable to save data: %w", err))
	}
	if p.gitInitialized {
		p.needsRestart(ctx, "Received an updated config from another device, please restart the application")
	}
}

func (p *Panel) needsRestart(
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

func (p *Panel) sendConfigViaGIT(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "sendConfigViaGIT")
	defer logger.Debugf(ctx, "/sendConfigViaGIT")

	p.gitSyncerMutex.Lock()
	defer p.gitSyncerMutex.Unlock()

	logger.Debugf(ctx, "sendConfigViaGIT: Lock() success")

	var newBytes bytes.Buffer
	latestSyncCommit := p.data.GitRepo.LatestSyncCommit
	p.data.GitRepo.LatestSyncCommit = ""
	err := writePanelData(ctx, &newBytes, p.data)
	p.data.GitRepo.LatestSyncCommit = latestSyncCommit
	if err != nil {
		return fmt.Errorf("unable to serialize the panel data: %w", err)
	}

	hash, err := p.gitStorage.Write(ctx, newBytes.Bytes())
	if errors.As(err, &repository.ErrNeedsRebase{}) {
		p.gitSync(ctx)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to write the config to the git repo: %w", err)
	}
	if !hash.IsZero() {
		p.setLastKnownGitCommitHash(hash)
		if err := p.saveDataToConfigFile(ctx); err != nil {
			return fmt.Errorf("unable to store the new commit hash in the config file: %w", err)
		}
	}

	return nil
}
