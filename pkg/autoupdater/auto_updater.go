package autoupdater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/go-github/v66/github"
)

type GitHub interface {
	ListReleases(ctx context.Context, owner, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error)
	ListReleaseAssets(ctx context.Context, owner, repo string, releaseID int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error)
	DownloadReleaseAsset(ctx context.Context, owner, repo string, assetID int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error)
}

type Updater interface {
	Update(ctx context.Context, updateInfo *Update, artifact io.Reader) error
}

type AutoUpdater struct {
	GitHub        GitHub
	Owner         string
	Repository    string
	ReleaseRegexp *regexp.Regexp
	AssetName     string
	Updater       Updater
}

func New(
	owner string,
	repository string,
	releaseRegexp *regexp.Regexp,
	assetName string,
	updater Updater,
) *AutoUpdater {
	return &AutoUpdater{
		GitHub:        github.NewClient(nil).Repositories,
		Owner:         owner,
		Repository:    repository,
		ReleaseRegexp: releaseRegexp,
		AssetName:     assetName,
		Updater:       updater,
	}
}

func (u *AutoUpdater) CheckForUpdates(
	ctx context.Context,
	currentCommit string,
	currentBuildDate time.Time,
) (_ret *Update, _err error) {
	logger.Debugf(ctx, "CheckForUpdates")
	defer func() { logger.Debugf(ctx, "/CheckForUpdates: %#+v %v", _ret, _err) }()
	releases, _, err := u.GitHub.ListReleases(ctx, u.Owner, u.Repository, &github.ListOptions{
		Page:    0,
		PerPage: 100,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of releases of github.com/%s/%s: %w", u.Owner, u.Repository, err)
	}
	for _, release := range releases {
		if release.Name == nil {
			continue
		}
		name := *release.Name
		isMatch := u.ReleaseRegexp.MatchString(name)
		logger.Debugf(ctx, "release '%s': isMatch:%v", name, isMatch)
		if !isMatch {
			continue
		}
		isNewer, err := u.isNewer(ctx, release, currentCommit, currentBuildDate)
		if err != nil {
			return nil, fmt.Errorf("unable to check if release '%#+v' is newer than the current version: %w", release, err)
		}
		if !isNewer {
			return nil, ErrNoUpdates{}
		}
		assets, _, err := u.GitHub.ListReleaseAssets(ctx, u.Owner, u.Repository, release.GetID(), &github.ListOptions{
			Page:    0,
			PerPage: 100,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to get the list of assets of release '%s': %w", release.GetName(), err)
		}
		for _, asset := range assets {
			if asset.GetName() == u.AssetName {
				return &Update{
					Updater: u,
					Release: release,
					Asset:   asset,
				}, nil
			}
		}
		return nil, ErrNoAsset{}
	}

	return nil, ErrNoUpdates{}
}

func (u *AutoUpdater) isNewer(
	ctx context.Context,
	release *github.RepositoryRelease,
	currentCommit string,
	currentBuildDate time.Time,
) (bool, error) {
	if release.CreatedAt == nil {
		return false, fmt.Errorf("field 'CreatedAt' is nil")
	}
	createdAt := *release.CreatedAt

	if release.TargetCommitish == nil {
		return false, fmt.Errorf("field 'TargetCommitish' is nil")
	}
	targetCommit := *release.TargetCommitish

	if strings.EqualFold(targetCommit, currentCommit) {
		logger.Debugf(ctx, "the same commit (%s), not updating", currentCommit)
		return false, nil
	}

	if !createdAt.After(currentBuildDate) {
		logger.Debugf(ctx, "too old build: %s <= %s", createdAt.Time.Format(time.RFC3339), currentBuildDate.Format(time.RFC3339))
		return false, nil
	}

	return true, nil
}
