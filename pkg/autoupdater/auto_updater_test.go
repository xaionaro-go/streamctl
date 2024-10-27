package autoupdater

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/google/go-github/v66/github"
	"github.com/stretchr/testify/require"
)

type MockGitHub struct {
	ListReleasesFunc         func(ctx context.Context, owner, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error)
	ListReleaseAssetsFunc    func(ctx context.Context, owner, repo string, id int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error)
	DownloadReleaseAssetFunc func(ctx context.Context, owner, repo string, id int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error)
}

func (g *MockGitHub) ListReleases(ctx context.Context, owner, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error) {
	return g.ListReleasesFunc(ctx, owner, repo, opts)
}

func (g *MockGitHub) ListReleaseAssets(ctx context.Context, owner, repo string, id int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error) {
	return g.ListReleaseAssetsFunc(ctx, owner, repo, id, opts)
}

func (g *MockGitHub) DownloadReleaseAsset(ctx context.Context, owner, repo string, id int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error) {
	return g.DownloadReleaseAssetFunc(ctx, owner, repo, id, followRedirectsClient)
}

type MockUpdater struct {
	UpdateFunc func(ctx context.Context, updateInfo *Update, artifact io.Reader) error
}

func (u *MockUpdater) Update(ctx context.Context, updateInfo *Update, artifact io.Reader) error {
	return u.UpdateFunc(ctx, updateInfo, artifact)
}

func TestAutoUpdater(t *testing.T) {
	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logger.LevelTrace))
	t.Run("positive", func(t *testing.T) {
		updateCallCount := 0
		u := New("owner", "repository", regexp.MustCompile("^release-channel-.*"), "asset", &MockUpdater{
			UpdateFunc: func(ctx context.Context, updateInfo *Update, artifact io.Reader) error {
				updateCallCount++
				return nil
			},
		})
		u.GitHub = &MockGitHub{
			ListReleasesFunc: func(ctx context.Context, owner string, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error) {
				return []*github.RepositoryRelease{
					{
						TargetCommitish: ptr("commit1"),
						Name:            ptr("test-release-channel-1"),
						CreatedAt:       &github.Timestamp{Time: time.Unix(10, 0)},
					},
					{
						TargetCommitish: ptr("commit2"),
						Name:            ptr("release-channel-2"),
						CreatedAt:       &github.Timestamp{Time: time.Unix(2, 0)},
					},
				}, &github.Response{}, nil
			},
			ListReleaseAssetsFunc: func(ctx context.Context, owner, repo string, id int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error) {
				return []*github.ReleaseAsset{{
					ID:   ptr(int64(1)),
					Name: ptr("asset"),
				}}, &github.Response{}, nil
			},
			DownloadReleaseAssetFunc: func(ctx context.Context, owner, repo string, id int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error) {
				return io.NopCloser(bytes.NewReader([]byte("content"))), "", nil
			},
		}
		update, err := u.CheckForUpdates(ctx, "commit0", time.Unix(1, 2))
		require.NoError(t, err)
		require.Equal(t, &Update{
			Updater: u,
			Release: &github.RepositoryRelease{
				TargetCommitish: ptr("commit2"),
				Name:            ptr("release-channel-2"),
				CreatedAt:       &github.Timestamp{Time: time.Unix(2, 0)},
			},
			Asset: &github.ReleaseAsset{
				ID:   ptr(int64(1)),
				Name: ptr("asset"),
			},
		}, update)
		require.Zero(t, updateCallCount)
		err = update.Apply(ctx)
		require.NoError(t, err)
	})
}
