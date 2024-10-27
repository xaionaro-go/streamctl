package autoupdater

import (
	"context"
	"regexp"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/autoupdater"
	"github.com/xaionaro-go/streamctl/pkg/streampanel"
)

type AutoUpdater struct {
	*autoupdater.AutoUpdater
	GitCommit     string
	BuildDate     time.Time
	BeforeUpgrade func()
	AfterUpgrade  func()
}

var _ streampanel.AutoUpdater = (*AutoUpdater)(nil)
var _ autoupdater.Updater = (*AutoUpdater)(nil)

func New(
	gitCommit string,
	buildDate time.Time,
	beforeUpgrade func(),
	afterUpgrade func(),
) *AutoUpdater {
	u := &AutoUpdater{
		GitCommit:     gitCommit,
		BuildDate:     buildDate,
		BeforeUpgrade: beforeUpgrade,
		AfterUpgrade:  afterUpgrade,
	}
	u.AutoUpdater = autoupdater.New("xaionaro-go", "streamctl", regexp.MustCompile("^unstable-"), assetName, u)
	return u
}

func (u *AutoUpdater) CheckForUpdates(ctx context.Context) (streampanel.Update, error) {
	update, err := u.AutoUpdater.CheckForUpdates(ctx, u.GitCommit, u.BuildDate)
	if err != nil {
		return nil, err
	}

	return &Update{Update: update}, nil
}
