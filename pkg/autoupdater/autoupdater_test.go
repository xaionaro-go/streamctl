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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockUpdater struct {
	version string
}

func (m *mockUpdater) CheckForUpdates(ctx context.Context) (bool, string, error) {
	if m.version == "old" {
		return true, "new", nil
	}
	return false, "", nil
}

type MockUpdater struct {
	UpdateFunc func(ctx context.Context, updateInfo *Update, artifact io.Reader, _ ProgressBar) error
}

func (m *MockUpdater) Update(ctx context.Context, updateInfo *Update, artifact io.Reader, pb ProgressBar) error {
	return m.UpdateFunc(ctx, updateInfo, artifact, pb)
}

type MockGitHub struct {
	ListReleasesFunc         func(ctx context.Context, owner string, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error)
	ListReleaseAssetsFunc    func(ctx context.Context, owner, repo string, id int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error)
	DownloadReleaseAssetFunc func(ctx context.Context, owner, repo string, id int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error)
}

func (m *MockGitHub) ListReleases(ctx context.Context, owner string, repo string, opts *github.ListOptions) ([]*github.RepositoryRelease, *github.Response, error) {
	return m.ListReleasesFunc(ctx, owner, repo, opts)
}

func (m *MockGitHub) ListReleaseAssets(ctx context.Context, owner, repo string, id int64, opts *github.ListOptions) ([]*github.ReleaseAsset, *github.Response, error) {
	return m.ListReleaseAssetsFunc(ctx, owner, repo, id, opts)
}

func (m *MockGitHub) DownloadReleaseAsset(ctx context.Context, owner, repo string, id int64, followRedirectsClient *http.Client) (rc io.ReadCloser, redirectURL string, err error) {
	return m.DownloadReleaseAssetFunc(ctx, owner, repo, id, followRedirectsClient)
}

func TestAutoUpdaterCheck(t *testing.T) {
	u := &mockUpdater{version: "old"}
	hasUpdate, newVer, err := u.CheckForUpdates(context.Background())
	require.NoError(t, err)
	assert.True(t, hasUpdate)
	assert.Equal(t, "new", newVer)
}

func TestAutoUpdaterProgressUI(t *testing.T) {
	// Verify that ProgressBar interface can be used.
	var _ ProgressBar = (*mockProgressBar)(nil)
}

type mockProgressBar struct{}

func (m *mockProgressBar) SetProgress(float64) {}
func (m *mockProgressBar) SetStatus(string)    {}
func (m *mockProgressBar) Close() error        { return nil }

func TestAutoUpdater(t *testing.T) {
	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logger.LevelTrace))
	t.Run("positive", func(t *testing.T) {
		updateCallCount := 0
		u := New("owner", "repository", regexp.MustCompile("^release-channel-.*"), "asset", &MockUpdater{
			UpdateFunc: func(ctx context.Context, updateInfo *Update, artifact io.Reader, _ ProgressBar) error {
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
		err = update.Apply(ctx, nil)
		require.NoError(t, err)
	})
}
