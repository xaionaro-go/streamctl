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

func (u *Update) Apply(ctx context.Context) error {
	logger.Debugf(ctx, "applying the update %#+v", u.Release)

	contentReader, redirectURL, err := u.Updater.GitHub.DownloadReleaseAsset(ctx, u.Updater.Owner, u.Updater.Repository, u.Asset.GetID(), nil)
	if err != nil {
		return fmt.Errorf("unable to download the asset '%#+v' from release '%#+v': %w", u.Asset, u.Release, err)
	}
	if redirectURL != "" {
		logger.Debugf(ctx, "received a redirection URL '%s'", redirectURL)
		resp, err := http.Get(redirectURL)
		if err != nil {
			return fmt.Errorf("unable to fetch '%s': %w", redirectURL, err)
		}
		contentReader = resp.Body
	}
	defer contentReader.Close()

	return u.Updater.Updater.Update(ctx, u, contentReader)
}
