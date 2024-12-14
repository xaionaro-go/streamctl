package streamd

import (
	"context"
	"crypto"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	eventbus "github.com/asaskevich/EventBus"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/p2p"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/repository"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xcontext"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type StreamControllers struct {
	OBS     *obs.OBS
	Twitch  *twitch.Twitch
	Kick    *kick.Kick
	YouTube *youtube.YouTube
}

type SaveConfigFunc func(context.Context, config.Config) error

type OBSInstanceID = streamtypes.OBSInstanceID

type OBSState = streamtypes.OBSState

type StreamD struct {
	UI ui.UI

	P2PNetwork p2p.P2P

	SaveConfigFunc SaveConfigFunc
	ConfigLock     xsync.Gorex
	Config         config.Config

	CacheLock xsync.Mutex
	Cache     *cache.Cache

	GitStorage *repository.GIT

	CancelGitSyncer context.CancelFunc
	GitSyncerMutex  xsync.Mutex
	GitInitialized  bool

	StreamControllers StreamControllers

	Variables sync.Map

	OAuthListenPortsLocker xsync.Mutex
	OAuthListenPorts       map[uint16]struct{}

	ControllersLocker xsync.RWMutex

	StreamServerLocker xsync.RWMutex
	StreamServer       streamserver.StreamServer

	StreamStatusCache *memoize.MemoizeData
	OBSState          OBSState

	EventBus eventbus.Bus

	TimersLocker xsync.Mutex
	NextTimerID  uint64
	Timers       map[api.TimerID]*Timer

	ImageHash xsync.Map[string, imageHash]

	Options OptionsAggregated

	closeCallback []closeCallback

	imageTakerLocker xsync.Mutex
	imageTakerCancel context.CancelFunc
	imageTakerWG     sync.WaitGroup

	obsRestarter *obsRestarter
}

type imageHash uint64

var _ api.StreamD = (*StreamD)(nil)

func New(
	cfg config.Config,
	ui ui.UI,
	saveCfgFunc SaveConfigFunc,
	b *belt.Belt,
	options ...Option,
) (_ret *StreamD, _err error) {
	ctx := belt.CtxWithBelt(context.TODO(), b)

	logger.Debugf(ctx, "New()")
	defer func() { logger.Debugf(ctx, "/New(): %#+v %v", _ret, _ret) }()

	err := convertConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the config: %w", err)
	}

	d := &StreamD{
		UI:                ui,
		SaveConfigFunc:    saveCfgFunc,
		Config:            cfg,
		Cache:             &cache.Cache{},
		OAuthListenPorts:  map[uint16]struct{}{},
		StreamStatusCache: memoize.NewMemoizeData(),
		EventBus:          eventbus.New(),
		OBSState: OBSState{
			VolumeMeters: map[string][][3]float64{},
		},
		Timers:  map[api.TimerID]*Timer{},
		Options: Options(options).Aggregate(),
	}

	err = d.readCache(ctx)
	if err != nil {
		logger.FromBelt(b).Errorf("unable to read cache: %v", err)
	}

	return d, nil
}

func (d *StreamD) Run(ctx context.Context) (_ret error) { // TODO: delete the fetchConfig parameter
	logger.Debugf(ctx, "StreamD.Run()")
	defer func() { logger.Debugf(ctx, "/StreamD.Run(): %v", _ret) }()

	if !d.StreamServerLocker.ManualTryLock(ctx) {
		return fmt.Errorf("somebody already locked StreamServerLocker")
	}
	defer d.StreamServerLocker.ManualUnlock(ctx)

	if !d.ControllersLocker.ManualTryLock(ctx) {
		return fmt.Errorf("somebody already locked ControllersLocker")
	}
	defer d.ControllersLocker.ManualUnlock(ctx)

	err := d.secretsProviderUpdater(ctx)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the secrets updater: %w", err))
	}

	d.UI.SetStatus("Initializing streaming backends...")
	if err := d.EXPERIMENTAL_ReinitStreamControllers(ctx); err != nil {
		return fmt.Errorf("unable to initialize stream controllers: %w", err)
	}

	d.UI.SetStatus("Pre-downloading user data from streaming backends...")

	if err := d.InitCache(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize cache: %w", err))
	}

	d.UI.SetStatus("Initializing StreamServer...")
	if err := d.initStreamServer(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the stream server: %w", err))
	}

	d.UI.SetStatus("Starting the image taker...")
	if err := d.initImageTaker(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the image taker: %w", err))
	}

	d.UI.SetStatus("P2P network...")
	if err := d.initP2P(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the P2P network: %w", err))
	}

	d.UI.SetStatus("OBS restarter...")
	if err := d.initOBSRestarter(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the OBS restarter: %w", err))
	}

	d.UI.SetStatus("Initializing UI...")
	return nil
}

func (d *StreamD) secretsProviderUpdater(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "secretsProviderUpdater")
	defer logger.Debugf(ctx, "/secretsProviderUpdater: %v", _err)

	cfgChangeCh, err := eventSubToChan[api.DiffConfig](ctx, d)
	if err != nil {
		return fmt.Errorf("unable to subscribe to config changes: %w", err)
	}

	d.ConfigLock.Do(ctx, func() {
		observability.SecretsProviderFromCtx(ctx).(*observability.SecretsStaticProvider).ParseSecretsFrom(d.Config)
	})
	logger.Debugf(ctx, "updated the secrets")

	observability.Go(ctx, func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cfgChangeCh:
				d.ConfigLock.Do(ctx, func() {
					observability.SecretsProviderFromCtx(ctx).(*observability.SecretsStaticProvider).ParseSecretsFrom(d.Config)
				})
				logger.Debugf(ctx, "updated the secrets")
			}
		}
	})
	return nil
}

func (d *StreamD) InitStreamServer(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "InitStreamServer")
	defer logger.Debugf(ctx, "/InitStreamServer: %v", _err)

	return xsync.DoA1R1(ctx, &d.ControllersLocker, d.initStreamServer, ctx)
}

func (d *StreamD) initStreamServer(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "initStreamServer")
	defer logger.Debugf(ctx, "/initStreamServer: %v", _err)

	d.StreamServer = streamserver.New(
		&d.Config.StreamServer,
		newPlatformsControllerAdapter(d),
		//newBrowserOpenerAdapter(d),
	)
	assert(d.StreamServer != nil)
	defer d.publishEvent(ctx, api.DiffStreamServers{})
	return d.StreamServer.Init(
		ctx,
		sstypes.InitOptionDefaultStreamPlayerOptions(d.streamPlayerOptions()),
	)
}

func (d *StreamD) streamPlayerOptions() sptypes.Options {
	return sptypes.Options{
		sptypes.OptionNotifierStart{
			d.notifyStreamPlayerStart,
		},
	}
}

func (d *StreamD) readCache(ctx context.Context) error {
	logger.Tracef(ctx, "readCache")
	defer logger.Tracef(ctx, "/readCache")

	d.Cache = &cache.Cache{}

	if d.Config.CachePath == nil {
		d.Config.CachePath = config.NewConfig().CachePath
		logger.Tracef(ctx, "setting the CachePath to default value '%s'", *d.Config.CachePath)
	}

	if *d.Config.CachePath == "" {
		logger.Tracef(ctx, "CachePath is empty, skipping")
		return nil
	}

	cachePath, err := xpath.Expand(*d.Config.CachePath)
	if err != nil {
		return fmt.Errorf("unable to expand path '%s': %w", *d.Config.CachePath, err)
	}

	if _, err := os.Stat(cachePath); os.IsNotExist(err) {
		logger.Debugf(ctx, "cache file does not exist")
		return nil
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
		logger.Tracef(ctx, "setting the CachePath to default value '%s'", *d.Config.CachePath)
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

func (d *StreamD) InitCache(ctx context.Context) error {
	logger.Tracef(ctx, "InitCache")
	defer logger.Tracef(ctx, "/InitCache")

	changedCache := false

	var wg sync.WaitGroup

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		_changedCache := d.initTwitchData(ctx)
		d.normalizeTwitchData()
		if _changedCache {
			changedCache = true
		}
	})

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		_changedCache := d.initYoutubeData(ctx)
		d.normalizeYoutubeData()
		if _changedCache {
			changedCache = true
		}
	})

	wg.Wait()
	if changedCache {
		err := d.writeCache(ctx)
		if err != nil {
			logger.Errorf(ctx, "unable to write cache into '%s': %v", *d.Config.CachePath, err)
		}
	}
	return nil
}

func (d *StreamD) setPlatformConfig(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	platCfg *streamcontrol.AbstractPlatformConfig,
) error {
	logger.Debugf(ctx, "setPlatformConfig('%s', '%#+v')", platID, platCfg)
	defer logger.Debugf(ctx, "endof setPlatformConfig('%s', '%#+v')", platID, platCfg)
	if d.Config.Backends == nil {
		d.Config.Backends = make(streamcontrol.Config)
	}
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

	allCategories, err := twitch.GetAllCategories(d.ctxForController(ctx))
	if err != nil {
		d.UI.DisplayError(err)
		return false
	}

	logger.FromCtx(ctx).Debugf("got categories: %#+v", allCategories)

	func() {
		d.CacheLock.Do(ctx, func() {
			d.Cache.Twitch.Categories = allCategories
		})
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

func (d *StreamD) SaveConfig(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &d.ConfigLock, d.saveConfig, ctx)
}

func (d *StreamD) saveConfig(ctx context.Context) error {
	err := d.SaveConfigFunc(ctx, d.Config)
	if err != nil {
		return err
	}

	return nil
}

func (d *StreamD) ResetCache(ctx context.Context) error {
	d.Cache = &cache.Cache{}
	return nil
}

func (d *StreamD) GetConfig(ctx context.Context) (*config.Config, error) {
	return ptr(d.Config), nil
}

func (d *StreamD) SetConfig(ctx context.Context, cfg *config.Config) error {
	return xsync.DoA2R1(ctx, &d.ConfigLock, d.setConfig, ctx, cfg)
}

func (d *StreamD) setConfig(ctx context.Context, cfg *config.Config) (_ret error) {
	dashboardCfgEqual := reflect.DeepEqual(d.Config.Dashboard, cfg.Dashboard)
	cfgEqual := reflect.DeepEqual(&d.Config, cfg)
	if cfgEqual {
		logger.Debugf(ctx, "config have no changed")
		return nil
	}
	defer func() {
		if _ret != nil {
			return
		}
		if !dashboardCfgEqual {
			logger.Debugf(ctx, "dashboard config changed")
			d.publishEvent(ctx, api.DiffDashboard{})
		}
		d.publishEvent(ctx, api.DiffConfig{})
	}()

	logger.Debugf(ctx, "SetConfig: %#+v", *cfg)
	err := convertConfig(*cfg)
	if err != nil {
		return fmt.Errorf("unable to convert the config: %w", err)
	}
	d.Config = *cfg

	if err := d.onUpdateConfig(ctx); err != nil {
		logger.Errorf(ctx, "onUpdateConfig: %v", err)
	}
	return nil
}

func (d *StreamD) onUpdateConfig(
	ctx context.Context,
) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	observability.Go(ctx, func() {
		defer wg.Done()
		errCh <- d.updateOBSRestarterConfig(ctx)
	})

	observability.Go(ctx, func() {
		wg.Wait()
		close(errCh)
	})

	var mErr *multierror.Error
	for err := range errCh {
		if err == nil {
			continue
		}
		mErr = multierror.Append(mErr, err)
	}
	return mErr.ErrorOrNil()
}

func (d *StreamD) IsBackendEnabled(
	ctx context.Context,
	id streamcontrol.PlatformName,
) (bool, error) {
	return xsync.RDoR2(ctx, &d.ControllersLocker, func() (bool, error) {
		switch id {
		case obs.ID:
			return d.StreamControllers.OBS != nil, nil
		case twitch.ID:
			return d.StreamControllers.Twitch != nil, nil
		case kick.ID:
			return d.StreamControllers.Kick != nil, nil
		case youtube.ID:
			return d.StreamControllers.YouTube != nil, nil
		default:
			return false, fmt.Errorf("unknown backend ID: '%s'", id)
		}
	})
}

func (d *StreamD) OBSOLETE_IsGITInitialized(ctx context.Context) (bool, error) {
	return d.GitStorage != nil, nil
}

func (d *StreamD) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) (_err error) {
	logger.Debugf(ctx, "StartStream(%s)", platID)
	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		defer func() { logger.Debugf(ctx, "/StartStream(%s): %v", platID, _err) }()
		defer d.publishEvent(ctx, api.DiffStreams{})

		defer func() {
			d.StreamStatusCache.InvalidateCache(ctx)
			if platID == youtube.ID {
				observability.Go(ctx, func() {
					now := time.Now()
					time.Sleep(10 * time.Second)
					for time.Since(now) < 5*time.Minute {
						d.StreamStatusCache.InvalidateCache(ctx)
						time.Sleep(20 * time.Second)
					}
				})
			}
		}()
		switch platID {
		case obs.ID:
			profile, err := streamcontrol.GetStreamProfile[obs.StreamProfile](ctx, profile)
			if err != nil {
				return fmt.Errorf("unable to convert the profile into OBS profile: %w", err)
			}
			err = d.StreamControllers.OBS.StartStream(
				d.ctxForController(ctx),
				title,
				description,
				*profile,
				customArgs...)
			if err != nil {
				return fmt.Errorf("unable to start the stream on OBS: %w", err)
			}
			return nil
		case twitch.ID:
			profile, err := streamcontrol.GetStreamProfile[twitch.StreamProfile](ctx, profile)
			if err != nil {
				return fmt.Errorf("unable to convert the profile into Twitch profile: %w", err)
			}
			err = d.StreamControllers.Twitch.StartStream(
				d.ctxForController(ctx),
				title,
				description,
				*profile,
				customArgs...)
			if err != nil {
				return fmt.Errorf("unable to start the stream on Twitch: %w", err)
			}
			return nil
		case kick.ID:
			profile, err := streamcontrol.GetStreamProfile[kick.StreamProfile](ctx, profile)
			if err != nil {
				return fmt.Errorf("unable to convert the profile into Twitch profile: %w", err)
			}
			err = d.StreamControllers.Kick.StartStream(
				d.ctxForController(ctx),
				title,
				description,
				*profile,
				customArgs...)
			if err != nil {
				return fmt.Errorf("unable to start the stream on Kick: %w", err)
			}
			return nil
		case youtube.ID:
			profile, err := streamcontrol.GetStreamProfile[youtube.StreamProfile](ctx, profile)
			if err != nil {
				return fmt.Errorf("unable to convert the profile into YouTube profile: %w", err)
			}
			err = d.StreamControllers.YouTube.StartStream(
				d.ctxForController(ctx),
				title,
				description,
				*profile,
				customArgs...)
			if err != nil {
				return fmt.Errorf("unable to start the stream on YouTube: %w", err)
			}

			// I don't know why, but if we don't open the livestream control page on YouTube
			// in the browser, then the stream does not want to start.
			//
			// And this bug is exacerbated by the fact that sometimes even if you just created
			// a stream, YouTube may report that you don't have this stream (some kind of
			// race condition on their side), so sometimes we need to wait and retry. Right
			// now we assume that the race condition cannot take more than ~25 seconds.
			deadline := time.Now().Add(30 * time.Second)
			for {
				status, err := d.GetStreamStatus(memoize.SetNoCache(ctx, true), youtube.ID)
				if err != nil {
					return fmt.Errorf("unable to get YouTube stream status: %w", err)
				}
				data := youtube.GetStreamStatusCustomData(status)
				bcID := getYTBroadcastID(data)
				if bcID == "" {
					err = fmt.Errorf("unable to get the broadcast ID from YouTube")
					if time.Now().Before(deadline) {
						delay := time.Second * 5
						logger.Warnf(ctx, "%v... waiting %v and trying again", err)
						time.Sleep(delay)
						continue
					}
					return err
				}
				url := fmt.Sprintf("https://studio.youtube.com/video/%s/livestreaming", bcID)
				err = d.UI.OpenBrowser(ctx, url)
				if err != nil {
					return fmt.Errorf("unable to open '%s' in browser: %w", url, err)
				}
				return nil
			}
		default:
			return fmt.Errorf("unexpected platform ID '%s'", platID)
		}
	})
}

func getYTBroadcastID(d youtube.StreamStatusCustomData) string {
	for _, bc := range d.ActiveBroadcasts {
		return bc.Id
	}
	for _, bc := range d.UpcomingBroadcasts {
		return bc.Id
	}
	return ""
}

func (d *StreamD) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	logger.Debugf(ctx, "EndStream(ctx, '%s')", platID)
	defer logger.Debugf(ctx, "/EndStream(ctx, '%s')", platID)

	defer d.publishEvent(ctx, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		defer d.StreamStatusCache.InvalidateCache(ctx)

		streamController, err := d.streamController(ctx, platID)
		if err != nil {
			return err
		}

		if streamController == nil {
			return fmt.Errorf("'%s' is not initialized", platID)
		}

		err = streamController.EndStream(ctx)
		if err != nil {
			return err
		}

		return nil
	})
}

func (d *StreamD) GetBackendInfo(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*api.BackendInfo, error) {
	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get stream controller for platform '%s': %w", platID, err)
	}

	caps := map[streamcontrol.Capability]struct{}{}
	for cap := streamcontrol.CapabilityUndefined + 1; cap < streamcontrol.EndOfCapability; cap++ {
		isCapable := ctrl.IsCapable(ctx, cap)
		if isCapable {
			caps[cap] = struct{}{}
		}
	}

	data, err := d.getBackendData(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get backend data: %w", err)
	}

	return &api.BackendInfo{
		Data:         data,
		Capabilities: caps,
	}, nil
}

func (d *StreamD) getBackendData(
	_ context.Context,
	platID streamcontrol.PlatformName,
) (any, error) {
	switch platID {
	case obs.ID:
		return api.BackendDataOBS{}, nil
	case twitch.ID:
		return api.BackendDataTwitch{Cache: d.Cache.Twitch}, nil
	case kick.ID:
		return api.BackendDataKick{Cache: d.Cache.Kick}, nil
	case youtube.ID:
		return api.BackendDataYouTube{Cache: d.Cache.Youtube}, nil
	default:
		return nil, fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) tryConnectTwitch(
	ctx context.Context,
) {
	if d.StreamControllers.Twitch != nil {
		return
	}

	if _, ok := d.Config.Backends[twitch.ID]; !ok {
		return
	}

	err := d.initTwitchBackend(ctx)
	errmon.ObserveErrorCtx(ctx, err)
}

func (d *StreamD) tryConnectKick(
	ctx context.Context,
) {
	if d.StreamControllers.Kick != nil {
		return
	}

	if _, ok := d.Config.Backends[kick.ID]; !ok {
		return
	}

	err := d.initKickBackend(ctx)
	errmon.ObserveErrorCtx(ctx, err)
}

func (d *StreamD) tryConnectYouTube(
	ctx context.Context,
) {
	if d.StreamControllers.YouTube != nil {
		return
	}

	if _, ok := d.Config.Backends[youtube.ID]; !ok {
		return
	}

	err := d.initYouTubeBackend(ctx)
	errmon.ObserveErrorCtx(ctx, err)
}

func (d *StreamD) streamController(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (streamcontrol.AbstractStreamController, error) {
	var result streamcontrol.AbstractStreamController
	switch platID {
	case obs.ID:
		if d.StreamControllers.OBS != nil {
			result = streamcontrol.ToAbstract(d.StreamControllers.OBS)
		}
	case twitch.ID:
		if d.StreamControllers.Twitch == nil {
			d.tryConnectTwitch(ctx)
		}
		if d.StreamControllers.Twitch != nil {
			result = streamcontrol.ToAbstract(d.StreamControllers.Twitch)
		}
	case kick.ID:
		if d.StreamControllers.Kick == nil {
			d.tryConnectKick(ctx)
		}
		if d.StreamControllers.Kick != nil {
			result = streamcontrol.ToAbstract(d.StreamControllers.Kick)
		}
	case youtube.ID:
		if d.StreamControllers.YouTube == nil {
			d.tryConnectYouTube(ctx)
		}
		if d.StreamControllers.YouTube != nil {
			result = streamcontrol.ToAbstract(d.StreamControllers.YouTube)
		}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	if result == nil {
		return nil, fmt.Errorf("controller '%s' is not initialized", platID)
	}
	return result, nil
}
func (d *StreamD) GetStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	cacheDuration := 5 * time.Second
	switch platID {
	case obs.ID:
		cacheDuration = 3 * time.Second
	case youtube.ID:
		cacheDuration = 5 * time.Minute // because of quota limits by YouTube
	}
	return memoize.Memoize(d.StreamStatusCache, d.getStreamStatus, ctx, platID, cacheDuration)
}

func (d *StreamD) getStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	return xsync.RDoR2(ctx, &d.ControllersLocker, func() (*streamcontrol.StreamStatus, error) {
		c, err := d.streamController(ctx, platID)
		if err != nil {
			return nil, err
		}

		if c == nil {
			return nil, fmt.Errorf("controller '%s' is not initialized", platID)
		}

		return c.GetStreamStatus(d.ctxForController(ctx))
	})
}

func (d *StreamD) SetTitle(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string,
) error {
	defer d.publishEvent(ctx, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		c, err := d.streamController(ctx, platID)
		if err != nil {
			return err
		}

		return c.SetTitle(d.ctxForController(ctx), title)
	})
}

func (d *StreamD) SetDescription(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	description string,
) error {
	defer d.publishEvent(ctx, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		c, err := d.streamController(ctx, platID)
		if err != nil {
			return err
		}

		return c.SetDescription(d.ctxForController(ctx), description)
	})
}

// TODO: delete this function (yes, it is not needed at all)
func (d *StreamD) ctxForController(ctx context.Context) context.Context {
	return ctx
}

func (d *StreamD) ApplyProfile(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	defer d.publishEvent(ctx, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		c, err := d.streamController(d.ctxForController(ctx), platID)
		if err != nil {
			return err
		}

		return c.ApplyProfile(d.ctxForController(ctx), profile, customArgs...)
	})
}

func (d *StreamD) UpdateStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	defer d.publishEvent(ctx, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.ControllersLocker, func() error {
		err := d.SetTitle(d.ctxForController(ctx), platID, title)
		if err != nil {
			return fmt.Errorf("unable to set the title: %w", err)
		}

		err = d.SetDescription(d.ctxForController(ctx), platID, description)
		if err != nil {
			return fmt.Errorf("unable to set the description: %w", err)
		}

		err = d.ApplyProfile(d.ctxForController(ctx), platID, profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to apply the profile: %w", err)
		}

		return nil
	})
}

func (d *StreamD) GetVariable(
	ctx context.Context,
	key consts.VarKey,
) ([]byte, error) {
	v, ok := d.Variables.Load(key)
	if !ok {
		return nil, ErrNoVariable{}
	}

	b, ok := v.([]byte)
	if !ok {
		return nil, ErrVariableWrongType{}
	}

	return b, nil
}

func (d *StreamD) GetVariableHash(
	ctx context.Context,
	key consts.VarKey,
	hashType crypto.Hash,
) ([]byte, error) {
	b, err := d.GetVariable(ctx, key)
	if err != nil {
		return nil, err
	}

	hasher := hashType.New()
	hasher.Write(b)
	hash := hasher.Sum(nil)
	return hash, nil
}

func (d *StreamD) SetVariable(
	ctx context.Context,
	key consts.VarKey,
	value []byte,
) error {
	logger.Tracef(ctx, "SetVariable(ctx, '%s', value [len == %d])", key, len(value))
	defer logger.Tracef(ctx, "/SetVariable(ctx, '%s', value [len == %d])", key, len(value))
	d.Variables.Store(key, value)
	return nil
}

func (d *StreamD) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	code := req.GetCode()
	if code == "" {
		return nil, fmt.Errorf("code is empty")
	}

	err := d.UI.OnSubmittedOAuthCode(
		ctx,
		streamcontrol.PlatformName(req.GetPlatID()),
		code,
	)
	if err != nil {
		return nil, err
	}

	return &streamd_grpc.SubmitOAuthCodeReply{}, nil
}

func (d *StreamD) AddOAuthListenPort(port uint16) {
	logger.Default().Debugf("AddOAuthListenPort(%d)", port)
	defer logger.Default().Debugf("/AddOAuthListenPort(%d)", port)
	ctx := context.TODO()
	d.OAuthListenPortsLocker.Do(ctx, func() {
		d.OAuthListenPorts[port] = struct{}{}
	})
}

func (d *StreamD) RemoveOAuthListenPort(port uint16) {
	logger.Default().Debugf("RemoveOAuthListenPort(%d)", port)
	defer logger.Default().Debugf("/RemoveOAuthListenPort(%d)", port)
	ctx := context.TODO()
	d.OAuthListenPortsLocker.Do(ctx, func() {
		delete(d.OAuthListenPorts, port)
	})
}

func (d *StreamD) GetOAuthListenPorts() []uint16 {
	ctx := context.TODO()
	return xsync.DoR1(ctx, &d.OAuthListenPortsLocker, d.getOAuthListenPorts)
}

func (d *StreamD) getOAuthListenPorts() []uint16 {
	var ports []uint16
	for k := range d.OAuthListenPorts {
		ports = append(ports, k)
	}

	sort.Slice(ports, func(i, j int) bool {
		return ports[i] < ports[j]
	})

	logger.Default().Debugf("oauth ports: %#+v", ports)
	return ports
}

func (d *StreamD) ListStreamServers(
	ctx context.Context,
) ([]api.StreamServer, error) {
	logger.Debugf(ctx, "ListStreamServers")
	defer logger.Debugf(ctx, "/ListStreamServers")

	return xsync.DoR2(ctx, &d.StreamServerLocker, func() ([]api.StreamServer, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}

		servers := d.StreamServer.ListServers(ctx)

		var result []api.StreamServer
		for idx, portSrv := range servers {
			srv := api.StreamServer{
				Config: streamportserver.Config{
					ProtocolSpecificConfig: portSrv.ProtocolSpecificConfig(),

					Type:       portSrv.Type(),
					ListenAddr: portSrv.ListenAddr(),
				},

				NumBytesConsumerWrote: portSrv.NumBytesConsumerWrote(),
				NumBytesProducerRead:  portSrv.NumBytesProducerRead(),
			}
			logger.Tracef(ctx, "srv[%d]: %#+v", idx, srv)
			result = append(result, srv)
		}

		return result, nil

	})
}

func (d *StreamD) StartStreamServer(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
	opts ...streamportserver.Option,
) error {
	logger.Debugf(ctx, "StartStreamServer")
	defer logger.Debugf(ctx, "/StartStreamServer")
	defer d.publishEvent(ctx, api.DiffStreamServers{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		_, err := d.StreamServer.StartServer(
			resetContextCancellers(ctx),
			serverType,
			listenAddr,
			opts...,
		)
		if err != nil {
			return fmt.Errorf("unable to start stream server: %w", err)
		}

		logger.Tracef(ctx, "new StreamServer.Servers config == %#+v", d.Config.StreamServer.PortServers)
		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) getStreamServerByListenAddr(
	ctx context.Context,
	listenAddr string,
) streamportserver.Server {
	for _, server := range d.StreamServer.ListServers(ctx) {
		if server.ListenAddr() == listenAddr {
			return server
		}
	}
	return nil
}

func (d *StreamD) StopStreamServer(
	ctx context.Context,
	listenAddr string,
) error {
	logger.Debugf(ctx, "StopStreamServer")
	defer logger.Debugf(ctx, "/StopStreamServer")
	defer d.publishEvent(ctx, api.DiffStreamServers{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		srv := d.getStreamServerByListenAddr(ctx, listenAddr)
		if srv == nil {
			return fmt.Errorf("have not found any stream listeners at %s", listenAddr)
		}

		err := d.StreamServer.StopServer(ctx, srv)
		if err != nil {
			return fmt.Errorf("unable to stop server %#+v: %w", srv, err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) AddIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "AddIncomingStream")
	defer logger.Debugf(ctx, "/AddIncomingStream")
	defer d.publishEvent(ctx, api.DiffIncomingStreams{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.AddIncomingStream(ctx, sstypes.StreamID(streamID))
		if err != nil {
			return fmt.Errorf("unable to add an incoming stream: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) RemoveIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "RemoveIncomingStream")
	defer logger.Debugf(ctx, "/RemoveIncomingStream")
	defer d.publishEvent(ctx, api.DiffIncomingStreams{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveIncomingStream(ctx, sstypes.StreamID(streamID))
		if err != nil {
			return fmt.Errorf("unable to remove an incoming stream: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) ListIncomingStreams(
	ctx context.Context,
) ([]api.IncomingStream, error) {
	logger.Debugf(ctx, "ListIncomingStreams")
	defer logger.Debugf(ctx, "/ListIncomingStreams")

	return xsync.DoR2(ctx, &d.StreamServerLocker, func() ([]api.IncomingStream, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		var result []api.IncomingStream
		for _, src := range d.StreamServer.ListIncomingStreams(ctx) {
			result = append(result, api.IncomingStream{
				StreamID: api.StreamID(src.StreamID),
			})
		}
		return result, nil
	})
}

func (d *StreamD) ListStreamDestinations(
	ctx context.Context,
) ([]api.StreamDestination, error) {
	logger.Debugf(ctx, "ListStreamDestinations")
	defer logger.Debugf(ctx, "/ListStreamDestinations")

	return xsync.DoR2(ctx, &d.StreamServerLocker, func() ([]api.StreamDestination, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		streamDestinations, err := d.StreamServer.ListStreamDestinations(ctx)
		if err != nil {
			return nil, err
		}
		c := make([]api.StreamDestination, 0, len(streamDestinations))
		for _, dst := range streamDestinations {
			c = append(c, api.StreamDestination{
				ID:        api.DestinationID(dst.ID),
				URL:       dst.URL,
				StreamKey: dst.StreamKey.Get(),
			})
		}
		return c, nil
	})
}

func (d *StreamD) AddStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	logger.Debugf(ctx, "AddStreamDestination")
	defer logger.Debugf(ctx, "/AddStreamDestination")
	defer d.publishEvent(ctx, api.DiffStreamDestinations{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.AddStreamDestination(
			resetContextCancellers(ctx),
			sstypes.DestinationID(destinationID),
			url,
			streamKey,
		)
		if err != nil {
			return fmt.Errorf("unable to add stream destination: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) UpdateStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
	streamKey string,
) error {
	logger.Debugf(ctx, "UpdateStreamDestination")
	defer logger.Debugf(ctx, "/UpdateStreamDestination")
	defer d.publishEvent(ctx, api.DiffStreamDestinations{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.UpdateStreamDestination(
			resetContextCancellers(ctx),
			sstypes.DestinationID(destinationID),
			url,
			streamKey,
		)
		if err != nil {
			return fmt.Errorf("unable to update stream destination: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) RemoveStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
) error {
	logger.Debugf(ctx, "RemoveStreamDestination")
	defer logger.Debugf(ctx, "/RemoveStreamDestination")
	defer d.publishEvent(ctx, api.DiffStreamDestinations{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveStreamDestination(ctx, sstypes.DestinationID(destinationID))
		if err != nil {
			return fmt.Errorf("unable to remove stream destination server: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) listStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	var result []api.StreamForward
	streamForwards, err := d.StreamServer.ListStreamForwards(ctx)
	if err != nil {
		return nil, err
	}
	for _, streamFwd := range streamForwards {
		item := api.StreamForward{
			Enabled:       streamFwd.Enabled,
			StreamID:      api.StreamID(streamFwd.StreamID),
			DestinationID: api.DestinationID(streamFwd.DestinationID),
			NumBytesWrote: streamFwd.NumBytesWrote,
			NumBytesRead:  streamFwd.NumBytesRead,
			Quirks:        streamFwd.Quirks,
		}
		result = append(result, item)
	}
	return result, nil
}

func (d *StreamD) ListStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	logger.Debugf(ctx, "ListStreamForwards")
	defer logger.Debugf(ctx, "/ListStreamForwards")

	return xsync.DoA1R2(ctx, &d.StreamServerLocker, d.listStreamForwards, ctx)
}

func (d *StreamD) AddStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	logger.Debugf(ctx, "AddStreamForward")
	defer logger.Debugf(ctx, "/AddStreamForward")
	defer d.publishEvent(ctx, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		_, err := d.StreamServer.AddStreamForward(
			resetContextCancellers(ctx),
			sstypes.StreamID(streamID),
			sstypes.DestinationID(destinationID),
			enabled,
			quirks,
		)
		if err != nil {
			return fmt.Errorf("unable to add the stream forwarding: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) UpdateStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	logger.Debugf(ctx, "AddStreamForward")
	defer logger.Debugf(ctx, "/AddStreamForward")
	defer d.publishEvent(ctx, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		_, err := d.StreamServer.UpdateStreamForward(
			resetContextCancellers(ctx),
			sstypes.StreamID(streamID),
			sstypes.DestinationID(destinationID),
			enabled,
			quirks,
		)
		if err != nil {
			return fmt.Errorf("unable to update the stream forwarding: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) RemoveStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
) error {
	logger.Debugf(ctx, "RemoveStreamForward")
	defer logger.Debugf(ctx, "/RemoveStreamForward")
	defer d.publishEvent(ctx, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveStreamForward(
			ctx,
			sstypes.StreamID(streamID),
			sstypes.DestinationID(destinationID),
		)
		if err != nil {
			return fmt.Errorf("unable to remove the stream forwarding: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func resetContextCancellers(ctx context.Context) context.Context {
	return belt.CtxWithBelt(context.Background(), belt.CtxBelt(ctx))
}

func (d *StreamD) WaitForStreamPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (<-chan struct{}, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		pubCh, err := d.StreamServer.WaitPublisherChan(ctx, streamID, false)
		if err != nil {
			return nil, err
		}

		ch := make(chan struct{})
		observability.Go(ctx, func() {
			select {
			case <-pubCh:
				close(ch)
			case <-ctx.Done():
			}
		})
		return ch, nil
	})
}

func (d *StreamD) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		defer d.publishEvent(ctx, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.AddStreamPlayer(
			ctx,
			streamID,
			playerType,
			disabled,
			streamPlaybackConfig,
			sstypes.StreamPlayerOptionDefaultOptions(d.streamPlayerOptions()),
		))
		result = multierror.Append(result, d.SaveConfig(ctx))
		return result.ErrorOrNil()
	})
}

func (d *StreamD) UpdateStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) (_err error) {
	logger.Debugf(
		ctx,
		"UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v)",
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
	)
	defer func() {
		logger.Debugf(
			ctx,
			"/UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v): %v",
			streamID,
			playerType,
			disabled,
			streamPlaybackConfig,
			_err,
		)
	}()
	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		defer d.publishEvent(ctx, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.UpdateStreamPlayer(
			ctx,
			streamID,
			playerType,
			disabled,
			streamPlaybackConfig,
			sstypes.StreamPlayerOptionDefaultOptions(d.streamPlayerOptions()),
		))
		result = multierror.Append(result, d.SaveConfig(ctx))
		return result.ErrorOrNil()
	})
}

func (d *StreamD) RemoveStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		defer d.publishEvent(ctx, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.RemoveStreamPlayer(
			ctx,
			streamID,
		))
		result = multierror.Append(result, d.SaveConfig(ctx))
		return result.ErrorOrNil()
	})
}

func (d *StreamD) ListStreamPlayers(
	ctx context.Context,
) ([]api.StreamPlayer, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() ([]api.StreamPlayer, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		result, err := d.StreamServer.ListStreamPlayers(ctx)
		if err != nil {
			return nil, err
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].StreamID < result[j].StreamID
		})
		return result, nil
	})
}

func (d *StreamD) GetStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (*api.StreamPlayer, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (*api.StreamPlayer, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		return d.StreamServer.GetStreamPlayer(ctx, streamID)
	})
}

func (d *StreamD) getActiveStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (player.Player, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (player.Player, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		return d.StreamServer.GetActiveStreamPlayer(ctx, streamID)
	})
}

func (d *StreamD) StreamPlayerProcessTitle(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return "", err
	}
	return streamPlayer.ProcessTitle(ctx)
}
func (d *StreamD) StreamPlayerOpenURL(
	ctx context.Context,
	streamID streamtypes.StreamID,
	link string,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return err
	}
	return streamPlayer.OpenURL(ctx, link)
}
func (d *StreamD) StreamPlayerGetLink(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return "", err
	}
	return streamPlayer.GetLink(ctx)
}
func (d *StreamD) StreamPlayerEndChan(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (<-chan struct{}, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return nil, err
	}
	return streamPlayer.EndChan(ctx)
}
func (d *StreamD) StreamPlayerIsEnded(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (bool, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return false, err
	}
	return streamPlayer.IsEnded(ctx)
}
func (d *StreamD) StreamPlayerGetPosition(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return streamPlayer.GetPosition(ctx)
}
func (d *StreamD) StreamPlayerGetLength(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return 0, err
	}
	return streamPlayer.GetLength(ctx)
}
func (d *StreamD) StreamPlayerSetSpeed(
	ctx context.Context,
	streamID streamtypes.StreamID,
	speed float64,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return err
	}
	return streamPlayer.SetSpeed(ctx, speed)
}
func (d *StreamD) StreamPlayerSetPause(
	ctx context.Context,
	streamID streamtypes.StreamID,
	pause bool,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return err
	}
	return streamPlayer.SetPause(ctx, pause)
}
func (d *StreamD) StreamPlayerStop(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return err
	}
	return streamPlayer.Stop(ctx)
}
func (d *StreamD) StreamPlayerClose(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamID)
	if err != nil {
		return err
	}
	return streamPlayer.Close(ctx)
}

func (d *StreamD) SetLoggingLevel(ctx context.Context, level logger.Level) error {
	observability.LogLevelFilter.SetLevel(level)
	d.UI.SetLoggingLevel(ctx, level)
	return nil
}

func (d *StreamD) GetLoggingLevel(ctx context.Context) (logger.Level, error) {
	return observability.LogLevelFilter.GetLevel(), nil
}

func (d *StreamD) AddTimer(
	ctx context.Context,
	triggerAt time.Time,
	action api.Action,
) (api.TimerID, error) {
	return xsync.DoA3R2(ctx, &d.TimersLocker, d.addTimer, ctx, triggerAt, action)
}

func (d *StreamD) addTimer(
	ctx context.Context,
	triggerAt time.Time,
	action api.Action,
) (api.TimerID, error) {
	logger.Debugf(ctx, "addTimer(ctx, %v, %v)", triggerAt, action)
	defer logger.Debugf(ctx, "/addTimer(ctx, %v, %v)", triggerAt, action)
	timerID := api.TimerID(atomic.AddUint64(&d.NextTimerID, 1))
	timer := NewTimer(d, timerID, triggerAt, action)
	timer.Start(xcontext.DetachDone(ctx))
	d.Timers[timerID] = timer
	return timerID, nil
}

func (d *StreamD) RemoveTimer(
	ctx context.Context,
	timerID api.TimerID,
) error {
	return xsync.DoA2R1(ctx, &d.TimersLocker, d.removeTimer, ctx, timerID)
}

func (d *StreamD) removeTimer(
	ctx context.Context,
	timerID api.TimerID,
) error {
	logger.Debugf(ctx, "removeTimer(ctx, %d)", timerID)
	defer logger.Debugf(ctx, "/removeTimer(ctx, %d)", timerID)
	timer, ok := d.Timers[timerID]
	if !ok {
		return fmt.Errorf("timer %d is not found", timerID)
	}
	delete(d.Timers, timerID)
	timer.Stop(ctx)
	return nil
}

func (d *StreamD) ListTimers(
	ctx context.Context,
) ([]api.Timer, error) {
	return xsync.DoA1R2(ctx, &d.TimersLocker, d.listTimers, ctx)
}

func (d *StreamD) listTimers(
	ctx context.Context,
) (_ret []api.Timer, _err error) {
	logger.Debugf(ctx, "listTimers")
	defer func() { logger.Debugf(ctx, "/listTimers: len(ret) == %d, err == %v", len(_ret), _err) }()
	result := make([]api.Timer, 0, len(d.Timers))
	for _, timer := range d.Timers {
		result = append(result, timer.Timer)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (d *StreamD) SubscribeToChatMessages(
	ctx context.Context,
) (<-chan api.ChatMessage, error) {
	return eventSubToChan[api.ChatMessage](ctx, d)
}

func (d *StreamD) SendChatMessage(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	message string,
) (_err error) {
	logger.Debugf(ctx, "SendChatMessage(ctx, '%s', '%s')", platID, message)
	defer func() { logger.Debugf(ctx, "/SendChatMessage(ctx, '%s', '%s'): %v", platID, message, _err) }()
	if message == "" {
		return nil
	}

	ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get stream controller for platform '%s': %w", platID, err)
	}

	err = ctrl.SendChatMessage(ctx, message)
	if err != nil {
		return fmt.Errorf("unable to send message '%s' to platform '%s': %w", message, platID, err)
	}

	return nil
}

func (d *StreamD) DialContext(
	ctx context.Context,
	network string,
	addr string,
) (net.Conn, error) {
	return net.Dial(network, addr)
}

func (p *StreamD) addCloseCallback(callback func() error, name string) {
	p.closeCallback = append(p.closeCallback, closeCallback{
		Callback: callback,
		Name:     name,
	})
}
