package autoupdater

import (
	"context"
	"fmt"
	"net/http"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/go-github/v66/github"
)

type Update struct {
	Updater *AutoUpdater
	Release *github.RepositoryRelease
	Asset   *github.ReleaseAsset
}

func (u *Update) Apply(
	ctx context.Context,
	progressBar ProgressBar,
) error {
	if progressBar == nil {
		progressBar = DummyProgressBar{}
	}
	progressBar.SetProgress(0)
	logger.Debugf(ctx, "applying the update %#+v", u.Release)

	contentReader, redirectURL, err := u.Updater.GitHub.DownloadReleaseAsset(ctx, u.Updater.Owner, u.Updater.Repository, u.Asset.GetID(), nil)
	if err != nil {
		return fmt.Errorf("unable to download the asset '%#+v' from release '%#+v': %w", u.Asset, u.Release, err)
	}
	progressBar.SetProgress(0.1)
	if redirectURL != "" {
		logger.Debugf(ctx, "received a redirection URL '%s'", redirectURL)
		resp, err := http.Get(redirectURL)
		if err != nil {
			return fmt.Errorf("unable to fetch '%s': %w", redirectURL, err)
		}
		contentReader = resp.Body
	}
	defer contentReader.Close()
	progressBar.SetProgress(0.2)

	return u.Updater.Updater.Update(ctx, u, contentReader, &delegatedProgressBar{
		AlreadyCompleted: 0.2,
		NextStageWeight:  0.8,
	})
}

type delegatedProgressBar struct {
	ProgressBar      ProgressBar
	AlreadyCompleted float64
	NextStageWeight  float64
}

func (p *delegatedProgressBar) SetProgress(v float64) {
	if p.ProgressBar == nil {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}

	p.ProgressBar.SetProgress(v/p.NextStageWeight + p.AlreadyCompleted)
}
