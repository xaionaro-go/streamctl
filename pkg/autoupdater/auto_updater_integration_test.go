//go:build integration_tests
// +build integration_tests

package autoupdater

import (
	"context"
	"io"
	"regexp"
	"testing"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	xlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	ctx := logger.CtxWithLogger(context.Background(), xlogrus.Default().WithLevel(logger.LevelTrace))
	updateCallCount := 0
	u := New("xaionaro-go", "streamctl", regexp.MustCompile("^unstable-.*"), "streampanel-linux-amd64", &MockUpdater{
		UpdateFunc: func(ctx context.Context, updateInfo *Update, artifact io.Reader, _ ProgressBar) error {
			updateCallCount++
			return nil
		},
	})
	update, err := u.CheckForUpdates(ctx, "commit0", time.Unix(1, 2))
	require.NoError(t, err)
	require.NotNil(t, update)
	require.Zero(t, updateCallCount)
	err = update.Apply(ctx, nil)
	require.NoError(t, err)
}
