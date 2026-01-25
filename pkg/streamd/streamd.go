package streamd

import (
	"context"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/eventbus"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/player/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/chatmessagesstorage"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/p2p"
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
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	sstypes "github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types/streamportserver"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xcontext"
	"github.com/xaionaro-go/xpath"
	"github.com/xaionaro-go/xsync"
)

type Accounts map[streamcontrol.AccountIDFullyQualified]streamcontrol.AbstractAccount

type SaveConfigFunc func(context.Context, config.Config) error

type OBSInstanceID = streamtypes.OBSInstanceID

type OBSState = streamtypes.OBSState

type StreamD struct {
	UI ui.UI

	P2PNetwork p2p.P2P

	SaveConfigFunc SaveConfigFunc
	ConfigLock     xsync.Gorex
	Config         config.Config
	ReadyChan      chan struct{}

	CacheLock xsync.Mutex
	Cache     *cache.Cache

	ChatMessagesStorage ChatMessageStorage

	GitStorage *repository.GIT

	CancelGitSyncer context.CancelFunc
	GitSyncerMutex  xsync.Mutex
	GitInitialized  bool

	AccountMap     Accounts
	ActiveProfiles map[streamcontrol.StreamIDFullyQualified]streamcontrol.ProfileName

	Variables sync.Map

	OAuthListenPortsLocker xsync.Mutex
	OAuthListenPorts       map[uint16]struct{}

	AccountsLocker xsync.RWMutex

	StreamServerLocker xsync.RWMutex
	StreamServer       streamserver.StreamServer

	StreamStatusCache *memoize.MemoizeData
	OBSState          OBSState

	EventBus *eventbus.EventBus

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

	llm *llm

	lastShoutoutAtLocker sync.Mutex
	lastShoutoutAt       map[config.ChatUserID]time.Time
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
		Options:             Options(options).Aggregate(),
		UI:                  ui,
		SaveConfigFunc:      saveCfgFunc,
		Config:              cfg,
		ChatMessagesStorage: chatmessagesstorage.New(cfg.GetChatMessageStorage(ctx)),
		Cache:               &cache.Cache{},
		OAuthListenPorts:    map[uint16]struct{}{},
		StreamStatusCache:   nil,
		EventBus:            eventbus.New(),
		OBSState: OBSState{
			VolumeMeters: map[string][][3]float64{},
		},
		Timers:         map[api.TimerID]*Timer{},
		ReadyChan:      make(chan struct{}),
		lastShoutoutAt: map[config.ChatUserID]time.Time{},
		AccountMap:     make(Accounts),
		ActiveProfiles: make(map[streamcontrol.StreamIDFullyQualified]streamcontrol.ProfileName),
	}
	d.StreamStatusCache = memoize.NewMemoizeData()

	return d, nil
}

func (d *StreamD) Run(ctx context.Context) (_ret error) { // TODO: delete the fetchConfig parameter
	fmt.Println("StreamD.Run: starting")
	logger.Debugf(ctx, "StreamD.Run()")
	defer func() { logger.Debugf(ctx, "/StreamD.Run(): %v", _ret) }()

	if err := d.ChatMessagesStorage.Load(ctx); err != nil {
		logger.Errorf(ctx, "unable to read the chat messages: %v", err)
	}

	err := d.readCache(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to read cache: %v", err)
	}

	err = d.secretsProviderUpdater(ctx)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the secrets updater: %w", err))
	}

	fmt.Println("StreamD.Run: Initializing streaming backends")
	d.UI.SetStatus("Initializing streaming backends...")
	if err := d.EXPERIMENTAL_ReinitStreamControllers(ctx); err != nil {
		return fmt.Errorf("unable to initialize stream controllers: %w", err)
	}

	fmt.Println("StreamD.Run: Pre-downloading user data")
	d.UI.SetStatus("Pre-downloading user data from streaming backends...")

	if err := d.InitCache(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize cache: %w", err))
	}

	fmt.Println("StreamD.Run: Initializing StreamServer")
	d.UI.SetStatus("Initializing StreamServer...")
	if err := d.initStreamServer(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the stream server: %w", err))
	}

	fmt.Println("StreamD.Run: Starting the image taker")
	d.UI.SetStatus("Starting the image taker...")
	if err := d.initImageTaker(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the image taker: %w", err))
	}

	fmt.Println("StreamD.Run: P2P network")
	d.UI.SetStatus("P2P network...")
	if err := d.initP2P(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the P2P network: %w", err))
	}

	fmt.Println("StreamD.Run: Initializing chat messages storage")
	d.UI.SetStatus("Initializing chat messages storage...")
	if err := d.initChatMessagesStorage(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the chat messages storage: %w", err))
	}

	fmt.Println("StreamD.Run: OBS restarter")
	d.UI.SetStatus("OBS restarter...")
	if err := d.initOBSRestarter(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the OBS restarter: %w", err))
	}

	fmt.Println("StreamD.Run: LLMs")
	d.UI.SetStatus("LLMs...")
	if err := d.initLLMs(ctx); err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the LLMs: %w", err))
	}

	fmt.Println("StreamD.Run: Initializing UI")
	d.UI.SetStatus("Initializing UI...")
	close(d.ReadyChan)
	fmt.Println("StreamD.Run: Ready")
	return nil
}

func (d *StreamD) initChatMessagesStorage(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "initChatMessagesStorage")
	defer logger.Debugf(ctx, "/initChatMessagesStorage: %v", _err)

	observability.Go(ctx, func(ctx context.Context) {
		logger.Debugf(ctx, "initChatMessagesStorage-refresherLoop")
		defer logger.Debugf(ctx, "/initChatMessagesStorage-refresherLoop")
		t := clock.Get().Ticker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			err := d.ChatMessagesStorage.Store(ctx)
			if err != nil {
				d.UI.DisplayError(fmt.Errorf("unable to store the chat messages: %w", err))
			}
		}
	})

	return nil
}

func (d *StreamD) secretsProviderUpdater(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "secretsProviderUpdater")
	defer logger.Debugf(ctx, "/secretsProviderUpdater: %v", _err)

	cfgChangeCh, err := eventSubToChan[api.DiffConfig](ctx, d.EventBus, 1000, nil)
	if err != nil {
		return fmt.Errorf("unable to subscribe to config changes: %w", err)
	}

	d.ConfigLock.Do(ctx, func() {
		observability.SecretsProviderFromCtx(ctx).(*observability.SecretsStaticProvider).ParseSecretsFrom(d.Config)
	})
	logger.Debugf(ctx, "updated the secrets")

	observability.Go(ctx, func(ctx context.Context) {
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

	return xsync.DoA1R1(ctx, &d.AccountsLocker, d.initStreamServer, ctx)
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
	defer publishEvent(ctx, d.EventBus, api.DiffStreamServers{})
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

	dst := &cache.Cache{}
	err = cache.ReadCacheFromPath(ctx, cachePath, dst)
	if err != nil {
		return fmt.Errorf("unable to read cache file '%s': %w", *d.Config.CachePath, err)
	}
	d.Cache = dst

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

	src := d.Cache.Clone()
	err = cache.WriteCacheToPath(ctx, cachePath, *src)
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
	var mu sync.Mutex

	for _, handler := range platformBackendHandlers {
		if handler.InitCache == nil {
			continue
		}
		handler := handler
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			if handler.InitCache(ctx, d) {
				mu.Lock()
				changedCache = true
				mu.Unlock()
			}
		})
	}

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
	platID streamcontrol.PlatformID,
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

func (d *StreamD) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error {
	logger.Debugf(ctx, "EXPERIMENTAL_ReinitStreamControllers")
	defer logger.Debugf(ctx, "/EXPERIMENTAL_ReinitStreamControllers")

	for _, handler := range platformBackendHandlers {
		if handler.InitBackend == nil {
			continue
		}
		if err := handler.InitBackend(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

func (d *StreamD) getControllersByPlatform(
	platID streamcontrol.PlatformID,
) map[streamcontrol.AccountID]streamcontrol.AbstractAccount {
	return xsync.RDoR1(context.TODO(), &d.AccountsLocker, func() map[streamcontrol.AccountID]streamcontrol.AbstractAccount {
		return d.getControllersByPlatformNoLock(platID)
	})
}

func (d *StreamD) getControllersByPlatformNoLock(
	platID streamcontrol.PlatformID,
) map[streamcontrol.AccountID]streamcontrol.AbstractAccount {
	result := make(map[streamcontrol.AccountID]streamcontrol.AbstractAccount)
	for id, ctrl := range d.AccountMap {
		if id.PlatformID == platID {
			result[id.AccountID] = ctrl
		}
	}
	return result
}

func (d *StreamD) StreamControllers(
	ctx context.Context,
	platID streamcontrol.PlatformID,
) (map[streamcontrol.AccountID]streamcontrol.AbstractAccount, error) {
	return d.getControllersByPlatform(platID), nil
}

func (d *StreamD) setPlatformAccountConfig(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	accountID streamcontrol.AccountID,
	cfg *streamcontrol.AbstractPlatformConfig,
) error {
	return xsync.DoA1R1(ctx, &d.ConfigLock, func(ctx context.Context) error {
		platCfg := d.Config.Backends[platID]
		if platCfg == nil {
			return fmt.Errorf("platform config for '%s' is not found", platID)
		}
		if platCfg.Accounts == nil {
			platCfg.Accounts = make(map[streamcontrol.AccountID]streamcontrol.RawMessage)
		}
		platCfg.Accounts[accountID] = cfg.Accounts[accountID]
		return d.saveConfig(ctx)
	}, ctx)
}

func (d *StreamD) SaveConfig(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "SaveConfig")
	defer func() { logger.Debugf(ctx, "/SaveConfig: %v", _err) }()
	return xsync.DoA1R1(ctx, &d.ConfigLock, d.saveConfig, ctx)
}

func (d *StreamD) saveConfig(ctx context.Context) error {
	if d.SaveConfigFunc == nil {
		return nil
	}
	err := d.SaveConfigFunc(ctx, d.Config)
	if err != nil {
		return err
	}

	return nil
}

func (d *StreamD) ResetCache(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "ResetCache")
	defer func() { logger.Debugf(ctx, "/ResetCache: %v", _err) }()
	d.Cache = &cache.Cache{}
	return nil
}

func (d *StreamD) GetConfig(ctx context.Context) (*config.Config, error) {
	return ptr(d.Config), nil
}

func (d *StreamD) SetConfig(ctx context.Context, cfg *config.Config) error {
	return xsync.DoA2R1(ctx, &d.ConfigLock, d.setConfig, ctx, cfg)
}

func (d *StreamD) setConfig(ctx context.Context, cfg *config.Config) (_err error) {
	logger.Debugf(ctx, "setConfig(ctx, %#+v)", cfg)
	defer func() { logger.Debugf(ctx, "/setConfig(ctx, %#+v): %v", cfg, _err) }()
	dashboardCfgEqual := reflect.DeepEqual(d.Config.Dashboard, cfg.Dashboard)
	cfgEqual := reflect.DeepEqual(&d.Config, cfg)
	if cfgEqual {
		logger.Debugf(ctx, "config have no changed")
		return nil
	}
	defer func() {
		if _err != nil {
			return
		}
		if !dashboardCfgEqual {
			logger.Debugf(ctx, "dashboard config changed")
			publishEvent(ctx, d.EventBus, api.DiffDashboard{})
		}
		publishEvent(ctx, d.EventBus, api.DiffConfig{})
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
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		errCh <- d.updateOBSRestarterConfig(ctx)
	})

	observability.Go(ctx, func(ctx context.Context) {
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
	id streamcontrol.PlatformID,
) (_ret bool, _err error) {
	logger.Tracef(ctx, "IsBackendEnabled(ctx, '%s')", id)
	defer func() { logger.Tracef(ctx, "/IsBackendEnabled(ctx, '%s'): %v %v", id, _ret, _err) }()

	return xsync.RDoR2(ctx, &d.AccountsLocker, func() (_ret bool, _err error) {
		_, ctrl := d.getAnyControllerWithIDByPlatform(id)
		return ctrl != nil, nil
	})
}

func (d *StreamD) OBSOLETE_IsGITInitialized(ctx context.Context) (bool, error) {
	return d.GitStorage != nil, nil
}

func (d *StreamD) WaitStreamStartedByStreamSourceID(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) error {
	adapter := newPlatformsControllerAdapter(d)
	return adapter.WaitStreamStartedByStreamSourceID(ctx, streamID)
}

func (d *StreamD) GetBackendInfo(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	includeData bool,
) (_ret *api.BackendInfo, _err error) {
	logger.Tracef(ctx, "GetBackendInfo(ctx, '%s', %t)", platID, includeData)
	defer func() { logger.Tracef(ctx, "/GetBackendInfo(ctx, '%s', %t): %v %v", platID, includeData, _ret, _err) }()

	_, ctrl, err := d.streamController(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get stream controller for platform '%s': %w", platID, err)
	}

	caps := map[streamcontrol.Capability]struct{}{}
	for cap := streamcontrol.UndefinedCapability + 1; cap < streamcontrol.EndOfCapability; cap++ {
		isCapable := ctrl.IsCapable(ctx, cap)
		if isCapable {
			caps[cap] = struct{}{}
		}
	}

	result := &api.BackendInfo{
		Capabilities: caps,
	}

	if includeData {
		data, err := d.getBackendData(ctx, platID)
		if err != nil {
			return nil, fmt.Errorf("unable to get backend data: %w", err)
		}
		result.Data = data
	}

	return result, nil
}

func (d *StreamD) getBackendData(
	ctx context.Context,
	platID streamcontrol.PlatformID,
) (any, error) {
	handler, ok := platformBackendHandlers[platID]
	if !ok || handler.GetBackendData == nil {
		return nil, fmt.Errorf("unexpected platform ID '%s'", platID)
	}
	return handler.GetBackendData(ctx, d)
}

func (d *StreamD) tryConnectTwitch(
	ctx context.Context,
) {
	_, ctrl := d.getAnyControllerWithIDByPlatform(twitch.ID)
	if ctrl != nil {
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
	_, ctrl := d.getAnyControllerWithIDByPlatform(kick.ID)
	if ctrl != nil {
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
	_, ctrl := d.getAnyControllerWithIDByPlatform(youtube.ID)
	if ctrl != nil {
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
	platID streamcontrol.PlatformID,
) (streamcontrol.AccountID, streamcontrol.AbstractAccount, error) {
	ctx = belt.WithField(ctx, "controller", platID)
	var (
		accountID streamcontrol.AccountID
		result    streamcontrol.AbstractAccount
	)
	switch platID {
	case obs.ID:
		accountID, result = d.getAnyControllerWithIDByPlatform(obs.ID)
	case twitch.ID:
		accountID, result = d.getAnyControllerWithIDByPlatform(twitch.ID)
		if result == nil {
			d.tryConnectTwitch(ctx)
		}
		accountID, result = d.getAnyControllerWithIDByPlatform(twitch.ID)
	case kick.ID:
		accountID, result = d.getAnyControllerWithIDByPlatform(kick.ID)
		if result == nil {
			d.tryConnectKick(ctx)
		}
		accountID, result = d.getAnyControllerWithIDByPlatform(kick.ID)
	case youtube.ID:
		accountID, result = d.getAnyControllerWithIDByPlatform(youtube.ID)
		if result == nil {
			d.tryConnectYouTube(ctx)
		}
		accountID, result = d.getAnyControllerWithIDByPlatform(youtube.ID)
	default:
		return "", nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	if result == nil {
		return "", nil, fmt.Errorf("controller '%s' is not initialized", platID)
	}
	return accountID, result, nil
}

func (d *StreamD) streamControllers(
	ctx context.Context,
	platID streamcontrol.PlatformID,
) (map[streamcontrol.AccountID]streamcontrol.AbstractAccount, error) {
	ctx = belt.WithField(ctx, "controller", platID)
	var controllers map[streamcontrol.AccountID]streamcontrol.AbstractAccount
	switch platID {
	case obs.ID:
		controllers = d.getControllersByPlatformNoLock(obs.ID)
	case twitch.ID:
		controllers = d.getControllersByPlatformNoLock(twitch.ID)
		if len(controllers) == 0 {
			d.AccountsLocker.URDo(ctx, func() {
				d.tryConnectTwitch(ctx)
			})
			controllers = d.getControllersByPlatformNoLock(twitch.ID)
		}
	case kick.ID:
		controllers = d.getControllersByPlatformNoLock(kick.ID)
		if len(controllers) == 0 {
			d.AccountsLocker.URDo(ctx, func() {
				d.tryConnectKick(ctx)
			})
			controllers = d.getControllersByPlatformNoLock(kick.ID)
		}
	case youtube.ID:
		controllers = d.getControllersByPlatformNoLock(youtube.ID)
		if len(controllers) == 0 {
			d.AccountsLocker.URDo(ctx, func() {
				d.tryConnectYouTube(ctx)
			})
			controllers = d.getControllersByPlatformNoLock(youtube.ID)
		}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	if len(controllers) == 0 {
		return nil, fmt.Errorf("controller '%s' is not initialized", platID)
	}
	return controllers, nil
}

func (d *StreamD) getAnyControllerWithIDByPlatform(
	platID streamcontrol.PlatformID,
) (streamcontrol.AccountID, streamcontrol.AbstractAccount) {
	for id, ctrl := range d.AccountMap {
		if id.PlatformID == platID {
			return id.AccountID, ctrl
		}
	}
	return "", nil
}

func (d *StreamD) GetStreamStatus(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (*streamcontrol.StreamStatus, error) {
	platID := streamID.PlatformID
	var cacheDuration time.Duration
	switch platID {
	case obs.ID:
		cacheDuration = 3 * time.Second
	case twitch.ID:
		cacheDuration = 10 * time.Second
	case youtube.ID:
		cacheDuration = 5 * time.Minute // because of quota limits by YouTube
	}
	return memoize.Memoize(d.StreamStatusCache, d.getStreamStatus, ctx, streamID, cacheDuration)
}

func (d *StreamD) getStreamStatus(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
) (*streamcontrol.StreamStatus, error) {
	platID := streamID.PlatformID
	return xsync.RDoR2(ctx, &d.AccountsLocker, func() (*streamcontrol.StreamStatus, error) {
		controllers := d.getControllersByPlatformNoLock(platID)

		accountID := streamID.AccountID
		if accountID == "" {
			for accID := range controllers {
				if platCfg := d.Config.Backends[platID]; platCfg != nil {
					if accCfg, ok := platCfg.Accounts[accID]; ok {
						if !accCfg.IsEnabled() {
							continue
						}
					}
				}
				accountID = accID
				break
			}
		}

		c, ok := controllers[accountID]
		if !ok {
			return nil, fmt.Errorf("controller '%s' (account '%s') is not initialized", platID, accountID)
		}

		return c.GetStreamStatus(d.ctxForController(ctx), streamID.StreamID)
	})
}

func (d *StreamD) SetTitle(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	title string,
) (_err error) {
	platID := streamID.PlatformID
	logger.Debugf(ctx, "SetTitle: %s, %s", platID, title)
	defer logger.Debugf(ctx, "/SetTitle: %s, %s: %v", platID, title, _err)
	defer publishEvent(ctx, d.EventBus, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.AccountsLocker, func() error {
		controllers, err := d.streamControllers(d.ctxForController(ctx), platID)
		if err != nil {
			return err
		}

		var mErr *multierror.Error
		for accountID, c := range controllers {
			if streamID.AccountID != "" && streamID.AccountID != accountID {
				continue
			}
			if platCfg := d.Config.Backends[platID]; platCfg != nil {
				if accCfg, ok := platCfg.Accounts[accountID]; ok {
					if !accCfg.IsEnabled() {
						continue
					}
				}
			}

			err := c.SetTitle(d.ctxForController(ctx), streamID.StreamID, title)
			if err != nil {
				mErr = multierror.Append(mErr, fmt.Errorf("unable to set the title for account %s: %w", accountID, err))
			}
		}
		return mErr.ErrorOrNil()
	})
}

func (d *StreamD) SetDescription(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	description string,
) (_err error) {
	platID := streamID.PlatformID
	logger.Debugf(ctx, "SetDescription: %s, %s", platID, description)
	defer logger.Debugf(ctx, "/SetDescription: %s, %s: %v", platID, description, _err)
	defer publishEvent(ctx, d.EventBus, api.DiffStreams{})

	return xsync.RDoR1(ctx, &d.AccountsLocker, func() error {
		controllers, err := d.streamControllers(d.ctxForController(ctx), platID)
		if err != nil {
			return err
		}

		var mErr *multierror.Error
		for accountID, c := range controllers {
			if streamID.AccountID != "" && streamID.AccountID != accountID {
				continue
			}
			if platCfg := d.Config.Backends[platID]; platCfg != nil {
				if accCfg, ok := platCfg.Accounts[accountID]; ok {
					if !accCfg.IsEnabled() {
						continue
					}
				}
			}

			err := c.SetDescription(d.ctxForController(ctx), streamID.StreamID, description)
			if err != nil {
				mErr = multierror.Append(mErr, fmt.Errorf("unable to set the description for account %s: %w", accountID, err))
			}
		}
		return mErr.ErrorOrNil()
	})
}

// TODO: delete this function (yes, it is not needed at all)
func (d *StreamD) ctxForController(ctx context.Context) context.Context {
	return ctx
}

func (d *StreamD) ApplyProfile(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	profile streamcontrol.StreamProfile,
) (_err error) {
	platID := streamID.PlatformID
	logger.Debugf(ctx, "ApplyProfile: %s, %#+v", platID, profile)
	defer logger.Debugf(ctx, "/ApplyProfile: %s, %#+v: %v", platID, profile, _err)
	defer publishEvent(ctx, d.EventBus, api.DiffStreams{})

	var mErr *multierror.Error
	err := xsync.RDoR1(ctx, &d.AccountsLocker, func() error {
		controllers, err := d.streamControllers(d.ctxForController(ctx), platID)
		if err != nil {
			return err
		}

		for accountID, c := range controllers {
			if streamID.AccountID != "" && streamID.AccountID != accountID {
				continue
			}

			p := profile
			if platCfg := d.Config.Backends[platID]; platCfg != nil && streamID.StreamID != "" {
				if accCfg, ok := platCfg.Accounts[accountID]; ok {
					if !accCfg.IsEnabled() {
						continue
					}
				}
			}

			err = c.ApplyProfile(d.ctxForController(ctx), streamID.StreamID, p)
			if err != nil {
				mErr = multierror.Append(mErr, fmt.Errorf("unable to apply the profile for account %s: %w", accountID, err))
			}
		}
		return nil
	})
	if err != nil {
		mErr = multierror.Append(mErr, err)
	}

	err = xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return nil
		}
		forwards, err := d.StreamServer.ListStreamForwards(ctx)
		if err != nil {
			return err
		}
		sinks, err := d.StreamServer.ListStreamSinks(ctx)
		if err != nil {
			return err
		}
		sinkMap := make(map[sstypes.StreamSinkIDFullyQualified]sstypes.StreamSink)
		for _, sink := range sinks {
			sinkMap[sink.ID] = sink
		}

		enabledInProfile := streamcontrol.ToRawMessage(profile).IsEnabled()

		for _, forward := range forwards {
			sink, ok := sinkMap[forward.StreamSinkID]
			if !ok || sink.StreamID == nil {
				continue
			}

			if sink.StreamID.PlatformID != platID {
				continue
			}

			if streamID.AccountID != "" && sink.StreamID.AccountID != streamID.AccountID {
				continue
			}
			if streamID.StreamID != "" && sink.StreamID.StreamID != streamID.StreamID {
				continue
			}

			if forward.Enabled != enabledInProfile {
				_, err := d.StreamServer.UpdateStreamForward(ctx, forward.StreamSourceID, forward.StreamSinkID, enabledInProfile, forward.Encode, forward.Quirks)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf("unable to update forward %s -> %s: %w", forward.StreamSourceID, forward.StreamSinkID, err))
				}
			}
		}
		return nil
	})
	if err != nil {
		mErr = multierror.Append(mErr, err)
	}

	return mErr.ErrorOrNil()
}

func (d *StreamD) SetStreamActive(
	ctx context.Context,
	streamID streamcontrol.StreamIDFullyQualified,
	active bool,
) (_err error) {
	platID := streamID.PlatformID
	logger.Debugf(ctx, "SetStreamActive(%s, %v)", streamID, active)
	return xsync.RDoR1(ctx, &d.AccountsLocker, func() error {
		defer func() { logger.Debugf(ctx, "/SetStreamActive(%s, %v): %v", streamID, active, _err) }()
		defer publishEvent(ctx, d.EventBus, api.DiffStreams{})

		defer func() {
			d.StreamStatusCache.InvalidateCache(ctx)
			if active && platID == youtube.ID {
				observability.Go(ctx, func(ctx context.Context) {
					now := clock.Get().Now()
					clock.Get().Sleep(10 * time.Second)
					for time.Since(now) < 5*time.Minute {
						d.StreamStatusCache.InvalidateCache(ctx)
						clock.Get().Sleep(20 * time.Second)
					}
				})
			}
		}()

		controllers, err := d.streamControllers(ctx, platID)
		if err != nil {
			return err
		}

		var mErr *multierror.Error
		for accountID, ctrl := range controllers {
			if streamID.AccountID != "" && streamID.AccountID != accountID {
				continue
			}
			if platCfg := d.Config.Backends[platID]; platCfg != nil {
				if accCfg, ok := platCfg.Accounts[accountID]; ok {
					if !accCfg.IsEnabled() {
						continue
					}
				}
			}

			if active {
				err = ctrl.SetStreamActive(
					d.ctxForController(ctx),
					streamID.StreamID,
					true,
				)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf("unable to start the stream for account %s: %w", accountID, err))
					continue
				}

				if platID == youtube.ID {
					// I don't know why, but if we don't open the livestream control page on YouTube
					// in the browser, then the stream does not want to start.
					//
					// And this bug is exacerbated by the fact that sometimes even if you just created
					// a stream, YouTube may report that you don't have this stream (some kind of
					// race condition on their side), so sometimes we need to wait and retry. Right
					// now we assume that the race condition cannot take more than ~25 seconds.
					deadline := clock.Get().Now().Add(30 * time.Second)
					for {
						status, err := ctrl.GetStreamStatus(
							d.ctxForController(memoize.SetNoCache(ctx, true)),
							streamID.StreamID,
						)
						data := youtube.GetStreamStatusCustomData(status)
						bcID := getYTBroadcastID(data)
						if bcID == "" {
							err = fmt.Errorf("unable to get the broadcast ID from YouTube for account %s", accountID)
							if clock.Get().Now().Before(deadline) {
								delay := time.Second * 5
								logger.Warnf(ctx, "%v... waiting %v and trying again", err)
								clock.Get().Sleep(delay)
								continue
							}
							mErr = multierror.Append(mErr, err)
							break
						}
						url := fmt.Sprintf("https://studio.youtube.com/video/%s/livestreaming", bcID)
						err = d.UI.OpenBrowser(ctx, url)
						if err != nil {
							mErr = multierror.Append(mErr, fmt.Errorf("unable to open '%s' in browser for account %s: %w", url, accountID, err))
						}
						break
					}
				}
			} else {
				cfg, err := d.GetConfig(ctx)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf("unable to get the config: %w", err))
					continue
				}

				if ctrl.IsCapable(ctx, streamcontrol.CapabilityIsChannelStreaming) && ctrl.IsCapable(ctx, streamcontrol.CapabilityRaid) {
					for _, userID := range cfg.Raid.AutoRaidOnStreamEnd {
						if userID.Platform != platID {
							continue
						}
						isStreaming, err := ctrl.IsChannelStreaming(ctx, userID.User)
						if err != nil {
							logger.Errorf(ctx, "unable to check if '%s' is streaming for account %s: %v", userID.User, accountID, err)
							continue
						}
						if !isStreaming {
							logger.Debugf(ctx, "checking if can raid to %v for account %s: user is not streaming", userID.User, accountID)
							continue
						}
						err = ctrl.RaidTo(ctx, streamcontrol.DefaultStreamID, userID.User)
						if err != nil {
							logger.Errorf(ctx, "unable to raid to '%s' for account %s: %v", userID.User, accountID, err)
							continue
						}

						if handler, ok := platformBackendHandlers[platID]; ok && handler.PostRaidWaitDuration != 0 {
							logger.Debugf(ctx, "sleeping for %v, to wait until Raid happens", handler.PostRaidWaitDuration)
							clock.Get().Sleep(handler.PostRaidWaitDuration)
						}
						break
					}
				}

				err = ctrl.SetStreamActive(ctx, streamID.StreamID, false)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf("unable to end the stream for account %s: %w", accountID, err))
				}
			}
		}
		return mErr.ErrorOrNil()
	})
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
		streamcontrol.PlatformID(req.GetPlatID()),
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

	slices.Sort(ports)

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
	defer publishEvent(ctx, d.EventBus, api.DiffStreamServers{})

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
	defer publishEvent(ctx, d.EventBus, api.DiffStreamServers{})

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

func (d *StreamD) AddStreamSource(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
) error {
	logger.Debugf(ctx, "AddStreamSource")
	defer logger.Debugf(ctx, "/AddStreamSource")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamSources{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.AddStreamSource(ctx, sstypes.StreamSourceID(streamSourceID))
		if err != nil {
			return fmt.Errorf("unable to add a stream source: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) RemoveStreamSource(
	ctx context.Context,
	streamSourceID api.StreamSourceID,
) error {
	logger.Debugf(ctx, "RemoveStreamSource")
	defer logger.Debugf(ctx, "/RemoveStreamSource")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamSources{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveStreamSource(ctx, sstypes.StreamSourceID(streamSourceID))
		if err != nil {
			return fmt.Errorf("unable to remove a stream source: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) ListStreamSources(
	ctx context.Context,
) (_ret []api.StreamSource, _err error) {
	logger.Debugf(ctx, "ListStreamSources")
	defer func() { logger.Debugf(ctx, "/ListStreamSources: %v", _err) }()
	if d == nil {
		return nil, fmt.Errorf("StreamD == nil")
	}

	return xsync.DoA1R2(ctx, &d.StreamServerLocker, d.listStreamSourcesNoLock, ctx)
}

func (d *StreamD) listStreamSourcesNoLock(
	ctx context.Context,
) (_ret []api.StreamSource, _err error) {
	logger.Debugf(ctx, "listStreamSourcesNoLock")
	defer func() { logger.Debugf(ctx, "/listStreamSourcesNoLock: %v", _err) }()

	if d.StreamServer == nil {
		return nil, fmt.Errorf("stream server is not initialized")
	}

	activeStreamSources, err := d.StreamServer.ActiveStreamSourceIDs()
	if err != nil {
		logger.Errorf(ctx, "unable to get the list of active stream sources: %w", err)
	}
	isActive := map[sstypes.StreamSourceID]struct{}{}
	for _, streamSourceID := range activeStreamSources {
		isActive[streamSourceID] = struct{}{}
	}

	var result []api.StreamSource
	for _, src := range d.StreamServer.ListStreamSources(ctx) {
		_, isActive := isActive[src.StreamSourceID]
		result = append(result, api.StreamSource{
			StreamSourceID: api.StreamSourceID(src.StreamSourceID),
			IsActive:       isActive,
		})
	}
	return result, nil
}

func (d *StreamD) ListStreamSinks(
	ctx context.Context,
) ([]api.StreamSink, error) {
	logger.Debugf(ctx, "ListStreamSinks")
	defer logger.Debugf(ctx, "/ListStreamSinks")

	var result []api.StreamSink
	err := xsync.RDoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		streamSinks, err := d.StreamServer.ListStreamSinks(ctx)
		if err != nil {
			return err
		}
		for _, sink := range streamSinks {
			result = append(result, api.StreamSink{
				ID: sink.ID,
				StreamSinkConfig: sstypes.StreamSinkConfig{
					URL:            sink.URL,
					StreamKey:      sink.StreamKey,
					StreamSourceID: sink.StreamID,
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sourceIDs, err := d.GetActiveStreamIDs(ctx)
	if err == nil {
		for _, id := range sourceIDs {
			cfg, err := d.GetStreamSinkConfig(ctx, id)
			if err != nil {
				logger.Errorf(ctx, "unable to get stream sink config for %v: %v", id, err)
				continue
			}
			result = append(result, api.StreamSink{
				ID: sstypes.NewStreamSinkIDFullyQualified(
					sstypes.StreamSinkTypeExternalPlatform,
					api.StreamSinkID(id.String()),
				),
				StreamSinkConfig: cfg,
			})
		}
	}

	_ = xsync.RDoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return nil
		}
		sources := d.StreamServer.ListStreamSources(ctx)
		for _, source := range sources {
			u, err := streamportserver.GetURLForLocalStreamID(ctx, d.StreamServer, source.StreamSourceID, nil)
			if err != nil {
				continue
			}
			result = append(result, api.StreamSink{
				ID: sstypes.NewStreamSinkIDFullyQualified(
					sstypes.StreamSinkTypeLocal,
					api.StreamSinkID(source.StreamSourceID),
				),
				StreamSinkConfig: sstypes.StreamSinkConfig{
					URL: u.String(),
				},
			})
		}
		return nil
	})

	return result, nil
}

func (d *StreamD) AddStreamSink(
	ctx context.Context,
	streamSinkID api.StreamSinkIDFullyQualified,
	sink sstypes.StreamSinkConfig,
) error {
	logger.Debugf(ctx, "AddStreamSink")
	defer logger.Debugf(ctx, "/AddStreamSink")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamSinks{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.AddStreamSink(
			resetContextCancellers(ctx),
			sstypes.StreamSinkIDFullyQualified(streamSinkID),
			sink,
		)
		if err != nil {
			return fmt.Errorf("unable to add stream sink: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) UpdateStreamSink(
	ctx context.Context,
	streamSinkID api.StreamSinkIDFullyQualified,
	sink sstypes.StreamSinkConfig,
) error {
	logger.Debugf(ctx, "UpdateStreamSink")
	defer logger.Debugf(ctx, "/UpdateStreamSink")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamSinks{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.UpdateStreamSink(
			resetContextCancellers(ctx),
			sstypes.StreamSinkIDFullyQualified(streamSinkID),
			sink,
		)
		if err != nil {
			return fmt.Errorf("unable to update stream sink: %w", err)
		}

		err = d.SaveConfig(ctx)
		if err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}

		return nil
	})
}

func (d *StreamD) RemoveStreamSink(
	ctx context.Context,
	streamSinkID api.StreamSinkIDFullyQualified,
) error {
	logger.Debugf(ctx, "RemoveStreamSink")
	defer logger.Debugf(ctx, "/RemoveStreamSink")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamSinks{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveStreamSink(ctx, sstypes.StreamSinkIDFullyQualified(streamSinkID))
		if err != nil {
			return fmt.Errorf("unable to remove stream sink: %w", err)
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
			Enabled:        streamFwd.Enabled,
			StreamSourceID: api.StreamSourceID(streamFwd.StreamSourceID),
			StreamSinkID:   api.StreamSinkIDFullyQualified(streamFwd.StreamSinkID),
			NumBytesWrote:  streamFwd.NumBytesWrote,
			NumBytesRead:   streamFwd.NumBytesRead,
			Encode:         streamFwd.Encode,
			Quirks:         streamFwd.Quirks,
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
	streamSourceID api.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
	enabled bool,
	encode sstypes.EncodeConfig,
	quirks api.StreamForwardingQuirks,
) error {
	logger.Debugf(ctx, "AddStreamForward")
	defer logger.Debugf(ctx, "/AddStreamForward")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		_, err := d.StreamServer.AddStreamForward(
			resetContextCancellers(ctx),
			sstypes.StreamSourceID(streamSourceID),
			sstypes.StreamSinkIDFullyQualified(streamSinkID),
			enabled,
			encode,
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
	streamSourceID api.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
	enabled bool,
	encode sstypes.EncodeConfig,
	quirks api.StreamForwardingQuirks,
) (_err error) {
	logger.Debugf(ctx, "UpdateStreamForward")
	defer func() { logger.Debugf(ctx, "/UpdateStreamForward: %v", _err) }()
	defer publishEvent(ctx, d.EventBus, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		_, err := d.StreamServer.UpdateStreamForward(
			resetContextCancellers(ctx),
			sstypes.StreamSourceID(streamSourceID),
			sstypes.StreamSinkIDFullyQualified(streamSinkID),
			enabled,
			encode,
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
	streamSourceID api.StreamSourceID,
	streamSinkID api.StreamSinkIDFullyQualified,
) error {
	logger.Debugf(ctx, "RemoveStreamForward")
	defer logger.Debugf(ctx, "/RemoveStreamForward")
	defer publishEvent(ctx, d.EventBus, api.DiffStreamForwards{})

	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		err := d.StreamServer.RemoveStreamForward(
			resetContextCancellers(ctx),
			sstypes.StreamSourceID(streamSourceID),
			sstypes.StreamSinkIDFullyQualified(streamSinkID),
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
	streamSourceID api.StreamSourceID,
	waitForNext bool,
) (<-chan struct{}, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (<-chan struct{}, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		pubCh, err := d.StreamServer.WaitPublisherChan(ctx, sstypes.StreamSourceID(streamSourceID), waitForNext)
		if err != nil {
			return nil, err
		}

		ch := make(chan struct{})
		observability.Go(ctx, func(ctx context.Context) {
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
	streamSourceID streamtypes.StreamSourceID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		defer publishEvent(ctx, d.EventBus, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.AddStreamPlayer(
			ctx,
			streamSourceID,
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
	streamSourceID streamtypes.StreamSourceID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) (_err error) {
	logger.Debugf(
		ctx,
		"UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v)",
		streamSourceID,
		playerType,
		disabled,
		streamPlaybackConfig,
	)
	defer func() {
		logger.Debugf(
			ctx,
			"/UpdateStreamPlayer(ctx, '%s', '%s', %v, %#+v): %v",
			streamSourceID,
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
		defer publishEvent(ctx, d.EventBus, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.UpdateStreamPlayer(
			ctx,
			streamSourceID,
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
	streamSourceID streamtypes.StreamSourceID,
) error {
	return xsync.DoR1(ctx, &d.StreamServerLocker, func() error {
		if d.StreamServer == nil {
			return fmt.Errorf("stream server is not initialized")
		}
		defer publishEvent(ctx, d.EventBus, api.DiffStreamPlayers{})
		var result *multierror.Error
		result = multierror.Append(result, d.StreamServer.RemoveStreamPlayer(
			ctx,
			streamSourceID,
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
			return result[i].StreamSourceID < result[j].StreamSourceID
		})
		return result, nil
	})
}

func (d *StreamD) GetStreamPlayer(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (*api.StreamPlayer, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (*api.StreamPlayer, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		return d.StreamServer.GetStreamPlayer(ctx, streamSourceID)
	})
}

func (d *StreamD) getActiveStreamPlayer(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (player.Player, error) {
	return xsync.DoR2(ctx, &d.StreamServerLocker, func() (player.Player, error) {
		if d.StreamServer == nil {
			return nil, fmt.Errorf("stream server is not initialized")
		}
		return d.StreamServer.GetActiveStreamPlayer(ctx, streamSourceID)
	})
}

func (d *StreamD) StreamPlayerProcessTitle(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (string, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return "", err
	}
	return streamPlayer.ProcessTitle(ctx)
}
func (d *StreamD) StreamPlayerOpenURL(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	link string,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return err
	}
	return streamPlayer.OpenURL(ctx, link)
}
func (d *StreamD) StreamPlayerGetLink(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (string, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return "", err
	}
	return streamPlayer.GetLink(ctx)
}
func (d *StreamD) StreamPlayerEndChan(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (<-chan struct{}, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return nil, err
	}
	return streamPlayer.EndChan(ctx)
}
func (d *StreamD) StreamPlayerIsEnded(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (bool, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return false, err
	}
	return streamPlayer.IsEnded(ctx)
}
func (d *StreamD) StreamPlayerGetPosition(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (time.Duration, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return 0, err
	}
	if streamPlayer == nil {
		return 0, fmt.Errorf("streamPlayer == nil")
	}
	return streamPlayer.GetPosition(ctx)
}
func (d *StreamD) StreamPlayerGetLength(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (time.Duration, error) {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return 0, err
	}
	return streamPlayer.GetLength(ctx)
}
func (d *StreamD) StreamPlayerGetLag(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) (time.Duration, time.Time, error) {
	now := clock.Get().Now()
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return 0, now, err
	}
	pos, err := streamPlayer.GetAudioPosition(ctx)
	if err != nil {
		return 0, now, fmt.Errorf("unable to get audio position: %w", err)
	}
	length, err := streamPlayer.GetLength(ctx)
	if err != nil {
		return 0, now, fmt.Errorf("unable to get stream length: %w", err)
	}
	lag := max(length-pos, 0)
	return lag, now, nil
}
func (d *StreamD) StreamPlayerSetSpeed(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	speed float64,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return err
	}
	return streamPlayer.SetSpeed(ctx, speed)
}
func (d *StreamD) StreamPlayerSetPause(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
	pause bool,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return err
	}
	return streamPlayer.SetPause(ctx, pause)
}
func (d *StreamD) StreamPlayerStop(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
	if err != nil {
		return err
	}
	return streamPlayer.Stop(ctx)
}
func (d *StreamD) StreamPlayerClose(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) error {
	streamPlayer, err := d.getActiveStreamPlayer(ctx, streamSourceID)
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

func (p *StreamD) Shoutout(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	userID streamcontrol.UserID,
) (_err error) {
	logger.Debugf(ctx, "Shoutout(ctx, '%s', '%s')", platID, userID)
	defer func() { logger.Debugf(ctx, "/Shoutout(ctx, '%s', '%s'): %v", platID, userID, _err) }()
	_, ctrl, err := p.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get a stream controller: %w", err)
	}
	return ctrl.Shoutout(ctx, streamcontrol.DefaultStreamID, userID)
}

func (p *StreamD) RaidTo(
	ctx context.Context,
	platID streamcontrol.PlatformID,
	userID streamcontrol.UserID,
) (_err error) {
	logger.Debugf(ctx, "RaidTo(ctx, '%s', '%s')", platID, userID)
	defer func() { logger.Debugf(ctx, "/RaidTo(ctx, '%s', '%s'): %v", platID, userID, _err) }()
	_, ctrl, err := p.streamController(ctx, platID)
	if err != nil {
		return fmt.Errorf("unable to get a stream controller: %w", err)
	}
	return ctrl.RaidTo(ctx, streamcontrol.DefaultStreamID, userID)
}
