package streamd

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/repository"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

type StreamControllers struct {
	OBS     *obs.OBS
	Twitch  *twitch.Twitch
	YouTube *youtube.YouTube
}

type StreamD struct {
	UI ui.UI

	ConfigPath string
	ConfigLock sync.Mutex
	Config     config.Config

	CacheLock sync.Mutex
	Cache     *cache.Cache

	GitStorage *repository.GIT

	CancelGitSyncer context.CancelFunc
	GitSyncerMutex  sync.Mutex
	GitInitialized  bool

	StreamControllers StreamControllers
}

var _ api.StreamD = (*StreamD)(nil)

func New(configPath string, ui ui.UI, b *belt.Belt) (*StreamD, error) {
	ctx := belt.CtxWithBelt(context.Background(), b)
	cfg, err := config.ReadOrCreateConfigFile(ctx, configPath)
	if err != nil {
		return nil, fmt.Errorf("unable to read or create config '%s': %w", configPath, err)
	}

	d := &StreamD{
		UI:         ui,
		ConfigPath: configPath,
		Config:     *cfg,
		Cache:      &cache.Cache{},
	}

	err = d.readCache(ctx)
	if err != nil {
		logger.FromBelt(b).Errorf("unable to read cache from '%s': %v", d.ConfigPath, err)
	}

	return d, nil
}

func (d *StreamD) readCache(ctx context.Context) error {
	logger.Tracef(ctx, "readCache")
	defer logger.Tracef(ctx, "/readCache")

	if d.Config.CachePath == nil {
		d.Config.CachePath = config.NewConfig().CachePath
		logger.Tracef(ctx, "setting the CachePath to default value '%s'", d.ConfigPath)
	}

	if *d.Config.CachePath == "" {
		logger.Tracef(ctx, "CachePath is empty, skipping")
		return nil
	}

	cachePath, err := xpath.Expand(*d.Config.CachePath)
	if err != nil {
		return fmt.Errorf("unable to expand path '%s': %w", *d.Config.CachePath, err)
	}

	err = cache.ReadCacheFromPath(ctx, cachePath, d.Cache)
	if err != nil {
		return fmt.Errorf("unable to read cache file '%s': %w", *d.Config.CachePath, err)
	}

	return nil
}

func (d *StreamD) writeCache(ctx context.Context) error {
	logger.Tracef(ctx, "writeCache")
	defer logger.Tracef(ctx, "/writeCache")

	if d.Config.CachePath == nil {
		d.Config.CachePath = config.NewConfig().CachePath
		logger.Tracef(ctx, "setting the CachePath to default value '%s'", d.ConfigPath)
	}

	if *d.Config.CachePath == "" {
		logger.Tracef(ctx, "CachePath is empty, skipping")
		return nil
	}

	cachePath, err := xpath.Expand(*d.Config.CachePath)
	if err != nil {
		return fmt.Errorf("unable to expand path '%s': %w", *d.Config.CachePath, err)
	}

	err = cache.WriteCacheToPath(ctx, cachePath, *d.Cache)
	if err != nil {
		return fmt.Errorf("unable to write to the cache file '%s': %w", *d.Config.CachePath, err)
	}

	return nil
}

func (d *StreamD) FetchConfig(ctx context.Context) error {
	logger.Tracef(ctx, "FetchConfig")
	defer logger.Tracef(ctx, "/FetchConfig")

	d.initGitIfNeeded(ctx)
	return nil
}

func (d *StreamD) InitCache(ctx context.Context) error {
	logger.Tracef(ctx, "InitCache")
	defer logger.Tracef(ctx, "/InitCache")

	changedCache := false

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_changedCache := d.initTwitchData(ctx)
		d.normalizeTwitchData()
		if _changedCache {
			changedCache = true
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		_changedCache := d.initYoutubeData(ctx)
		d.normalizeYoutubeData()
		if _changedCache {
			changedCache = true
		}
	}()

	wg.Wait()
	if changedCache {
		err := d.writeCache(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to write cache into '%s': %w", d.ConfigPath, err)
		}
	}
	return nil
}

func (d *StreamD) SetPlatformConfig(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	platCfg *streamcontrol.AbstractPlatformConfig,
) error {
	logger.Debugf(ctx, "SetPlatformConfig('%s', '%#+v')", platID, platCfg)
	defer logger.Debugf(ctx, "endof SetPlatformConfig('%s', '%#+v')", platID, platCfg)
	d.ConfigLock.Lock()
	defer d.ConfigLock.Unlock()
	d.Config.Backends[platID] = platCfg
	return d.SaveConfig(ctx)
}

func (d *StreamD) initTwitchData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Twitch data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Twitch data")

	if c := len(d.Cache.Twitch.Categories); c != 0 {
		logger.FromCtx(ctx).Debugf("already have categories (count: %d)", c)
		return false
	}

	twitch := d.StreamControllers.Twitch
	if twitch == nil {
		logger.FromCtx(ctx).Debugf("twitch controller is not initialized")
		return false
	}

	allCategories, err := twitch.GetAllCategories(ctx)
	if err != nil {
		d.UI.DisplayError(err)
		return false
	}

	logger.FromCtx(ctx).Debugf("got categories: %#+v", allCategories)

	func() {
		d.CacheLock.Lock()
		defer d.CacheLock.Unlock()
		d.Cache.Twitch.Categories = allCategories
	}()

	err = d.SaveConfig(ctx)
	errmon.ObserveErrorCtx(ctx, err)
	return true
}

func (d *StreamD) normalizeTwitchData() {
	s := d.Cache.Twitch.Categories
	sort.Slice(s, func(i, j int) bool {
		return s[i].Name < s[j].Name
	})
}

func (d *StreamD) initYoutubeData(ctx context.Context) bool {
	logger.FromCtx(ctx).Debugf("initializing Youtube data")
	defer logger.FromCtx(ctx).Debugf("endof initializing Youtube data")

	if c := len(d.Cache.Youtube.Broadcasts); c != 0 {
		logger.FromCtx(ctx).Debugf("already have broadcasts (count: %d)", c)
		return false
	}

	youtube := d.StreamControllers.YouTube
	if youtube == nil {
		logger.FromCtx(ctx).Debugf("youtube controller is not initialized")
		return false
	}

	broadcasts, err := youtube.ListBroadcasts(ctx)
	if err != nil {
		d.UI.DisplayError(err)
		return false
	}

	logger.FromCtx(ctx).Debugf("got broadcasts: %#+v", broadcasts)

	func() {
		d.CacheLock.Lock()
		defer d.CacheLock.Unlock()
		d.Cache.Youtube.Broadcasts = broadcasts
	}()

	err = d.SaveConfig(ctx)
	errmon.ObserveErrorCtx(ctx, err)
	return true
}

func (d *StreamD) normalizeYoutubeData() {
	s := d.Cache.Youtube.Broadcasts
	sort.Slice(s, func(i, j int) bool {
		return s[i].Snippet.Title < s[j].Snippet.Title
	})
}

func (d *StreamD) SaveConfig(ctx context.Context) error {
	err := d.saveDataToConfigFile(ctx)
	if err != nil {
		return err
	}

	go func() {
		if d.GitStorage != nil {
			err = d.sendConfigViaGIT(ctx)
			if err != nil {
				d.UI.DisplayError(fmt.Errorf("unable to send the config to the remote git repository: %w", err))
			}
		}
	}()

	return nil
}

func (d *StreamD) getExpandedDataPath() (string, error) {
	return xpath.Expand(d.ConfigPath)
}

func (d *StreamD) saveDataToConfigFile(ctx context.Context) error {
	dataPath, err := d.getExpandedDataPath()
	if err != nil {
		return fmt.Errorf("unable to get the path to the config file: %w", err)
	}

	err = config.WriteConfigToPath(ctx, dataPath, d.Config)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (d *StreamD) ResetCache(ctx context.Context) error {
	d.Cache.Twitch = cache.Twitch{}
	d.Cache.Youtube = cache.YouTube{}
	return nil
}

func (d *StreamD) GetConfig(ctx context.Context) (*config.Config, error) {
	return ptr(d.Config), nil
}

func (d *StreamD) SetConfig(ctx context.Context, cfg *config.Config) error {
	d.Config = *cfg
	return nil
}

func (d *StreamD) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error) {
	switch id {
	case obs.ID:
		return d.StreamControllers.OBS != nil, nil
	case twitch.ID:
		return d.StreamControllers.Twitch != nil, nil
	case youtube.ID:
		return d.StreamControllers.YouTube != nil, nil
	default:
		return false, fmt.Errorf("unknown backend ID: '%s'", id)
	}
}

func (d *StreamD) IsGITInitialized(ctx context.Context) (bool, error) {
	return d.GitStorage != nil, nil
}

func (d *StreamD) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	switch platID {
	case obs.ID:
		profile, err := streamcontrol.GetStreamProfile[obs.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into OBS profile: %w", err)
		}
		err = d.StreamControllers.OBS.StartStream(ctx, title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on OBS: %w", err)
		}
		return nil
	case twitch.ID:
		profile, err := streamcontrol.GetStreamProfile[twitch.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into Twitch profile: %w", err)
		}
		err = d.StreamControllers.Twitch.StartStream(ctx, title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on Twitch: %w", err)
		}
		return nil
	case youtube.ID:
		profile, err := streamcontrol.GetStreamProfile[youtube.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into YouTube profile: %w", err)
		}
		err = d.StreamControllers.YouTube.StartStream(ctx, title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on YouTube: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	switch platID {
	case obs.ID:
		return d.StreamControllers.OBS.EndStream(ctx)
	case twitch.ID:
		return d.StreamControllers.Twitch.EndStream(ctx)
	case youtube.ID:
		return d.StreamControllers.YouTube.EndStream(ctx)
	default:
		return fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error) {
	switch platID {
	case obs.ID:
		return api.BackendDataOBS{}, nil
	case twitch.ID:
		return api.BackendDataTwitch{Cache: d.Cache.Twitch}, nil
	case youtube.ID:
		return api.BackendDataYouTube{Cache: d.Cache.Youtube}, nil
	default:
		return nil, fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) Restart(ctx context.Context) error {
	d.UI.Restart(ctx, "A restart was requested")
	return nil
}
