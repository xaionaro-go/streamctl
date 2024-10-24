package streamd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/repository"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

const gitRepoPanelConfigFileName = "streampanel.yaml"
const gitCommitterName = "streampanel"
const gitCommitterEmail = "streampanel@noemail.invalid"

func (d *StreamD) sendConfigViaGIT(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "sendConfigViaGIT")
	defer logger.Debugf(ctx, "/sendConfigViaGIT")
	return xsync.DoA1R1(ctx, &d.GitSyncerMutex, d.sendConfigViaGITNoLock, ctx)
}

func (d *StreamD) sendConfigViaGITNoLock(
	ctx context.Context,
) error {
	var newBytes bytes.Buffer
	latestSyncCommit := d.Config.GitRepo.LatestSyncCommit
	d.Config.GitRepo.LatestSyncCommit = ""
	_, err := d.Config.WriteTo(&newBytes)
	d.Config.GitRepo.LatestSyncCommit = latestSyncCommit
	if err != nil {
		return fmt.Errorf("unable to serialize the panel data: %w", err)
	}

	hash, err := d.GitStorage.Write(ctx, newBytes.Bytes())
	if errors.As(err, &repository.ErrNeedsRebase{}) {
		d.gitSync(ctx)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to write the config to the git repo: %w", err)
	}
	if !hash.IsZero() {
		d.setLastKnownGitCommitHash(hash)
		if err := d.SaveConfigFunc(ctx, d.Config); err != nil {
			return fmt.Errorf("unable to store the new commit hash in the config file: %w", err)
		}
	}

	return nil
}

func (d *StreamD) gitSync(ctx context.Context) {
	logger.Debugf(ctx, "gitSync")
	defer logger.Debugf(ctx, "/gitSync")
	xsync.DoA1(ctx, &d.GitSyncerMutex, d.gitSyncNoLock, ctx)
}

func (d *StreamD) gitSyncNoLock(ctx context.Context) {
	logger.Debugf(ctx, "last_known_commit: %s", d.Config.GitRepo.LatestSyncCommit)
	err := d.GitStorage.Pull(
		ctx,
		d.getLastKnownGitCommitHash(),
		func(ctx context.Context, commitHash plumbing.Hash, b []byte) {
			panelData := config.NewConfig()
			_, err := panelData.Read(b)
			if err != nil {
				d.UI.DisplayError(fmt.Errorf("unable to read panel data: %w", err))
				return
			}
			panelData.GitRepo.LatestSyncCommit = commitHash.String()
			d.onConfigUpdateViaGIT(ctx, &panelData)
		},
	)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
	}
}

func (d *StreamD) setLastKnownGitCommitHash(newHash plumbing.Hash) {
	d.Config.GitRepo.LatestSyncCommit = newHash.String()
}

func (d *StreamD) getLastKnownGitCommitHash() plumbing.Hash {
	return plumbing.NewHash(d.Config.GitRepo.LatestSyncCommit)
}

func (d *StreamD) onConfigUpdateViaGIT(ctx context.Context, cfg *config.Config) {
	err := convertConfig(*cfg)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to convert the config: %w", err))
		return
	}
	d.Config = *cfg
	err = d.SaveConfig(ctx)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to save data: %w", err))
		return
	}
	if d.GitInitialized {
		d.UI.Restart(ctx, "Received an updated config from another device, please restart the application")
	}
}

func (d *StreamD) initGitIfNeeded(ctx context.Context) {
	logger.Debugf(ctx, "initGitIfNeeded: %#+v", d.Config.GitRepo)
	if d.Config.GitRepo.Enable != nil && !*d.Config.GitRepo.Enable {
		logger.Debugf(ctx, "git is disabled in the configuration")
		return
	}

	newUserData := false
	if d.Config.GitRepo.URL == "" || d.Config.GitRepo.PrivateKey.Get() == "" {
		newUserData = true
	}

	for {
		logger.Debugf(ctx, "attempting to configure a git storage")
		if newUserData {
			ok, url, privKey, err := d.UI.InputGitUserData(ctx)
			if err != nil {
				d.UI.DisplayError(fmt.Errorf("unable to input the git user data: %w", err))
				continue
			}
			d.Config.GitRepo.Enable = ptr(ok)
			d.Config.GitRepo.URL = url
			d.Config.GitRepo.PrivateKey.Set(string(privKey))
			if err := d.SaveConfig(ctx); err != nil {
				d.UI.DisplayError(err)
			}
			if !ok {
				d.deinitGitStorage(ctx)
				return
			}
		}

		logger.Debugf(ctx, "newGitStorage")
		gitStorage, err := repository.NewGit(
			ctx,
			d.Config.GitRepo.URL,
			[]byte(d.Config.GitRepo.PrivateKey.Get()),
			gitRepoPanelConfigFileName,
			gitCommitterName,
			gitCommitterEmail,
		)
		if err != nil {
			d.UI.DisplayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
			if newUserData {
				continue
			}
			d.Config.GitRepo.Enable = ptr(false)
			d.Config.GitRepo.URL = ""
			d.Config.GitRepo.PrivateKey.Set("")
			return
		}

		d.GitSyncerMutex.Do(ctx, func() {
			d.deinitGitStorage(ctx)
			if gitStorage == nil {
				panic("gitStorage == nil")
			}
			d.GitStorage = gitStorage
		})
		break
	}

	if d.GitStorage == nil {
		panic("d.GitStorage == nil")
	}
	d.startPeriodicGitSyncer(ctx)
	d.GitInitialized = true
}

func (d *StreamD) deinitGitStorage(_ context.Context) {
	if d.GitStorage != nil {
		err := d.GitStorage.Close()
		if err != nil {
			d.UI.DisplayError(err)
		}
		d.GitStorage = nil
	}
	if d.CancelGitSyncer != nil {
		d.CancelGitSyncer()
		d.CancelGitSyncer = nil
	}
}

func (d *StreamD) startPeriodicGitSyncer(ctx context.Context) {
	logger.Debugf(ctx, "startPeriodicGitSyncer")
	defer logger.Debugf(ctx, "/startPeriodicGitSyncer")

	d.GitSyncerMutex.Do(ctx, func() {
		if d.CancelGitSyncer != nil {
			logger.Debugf(ctx, "git syncer is already started")
			return
		}

		ctx, d.CancelGitSyncer = context.WithCancel(ctx)
	})

	d.gitSync(ctx)
	observability.Go(ctx, func() {
		err := d.sendConfigViaGIT(ctx)
		if err != nil {
			d.UI.DisplayError(
				fmt.Errorf("unable to send the config to the remote git repository: %w", err),
			)
		}

		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				d.GitSyncerMutex.Do(ctx, func() {
					d.CancelGitSyncer = nil
				})
				return
			case <-ticker.C:
			}

			d.gitSync(ctx)
		}
	})
}

func (d *StreamD) OBSOLETE_GitRelogin(ctx context.Context) error {
	alreadyLoggedIn := d.GitStorage != nil
	oldCfg := d.Config.GitRepo
	d.Config.GitRepo = config.GitRepoConfig{}
	d.initGitIfNeeded(ctx)
	if d.GitStorage == nil {
		d.Config.GitRepo = oldCfg
		if alreadyLoggedIn {
			d.initGitIfNeeded(ctx)
		}
	}
	return nil
}
