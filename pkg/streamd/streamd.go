package streamd

import (
	"context"
	"crypto"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	eventbus "github.com/asaskevich/EventBus"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/repository"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/cache"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/events"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamplayer"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xpath"
)

type StreamControllers struct {
	OBS     *obs.OBS
	Twitch  *twitch.Twitch
	YouTube *youtube.YouTube
}

type SaveConfigFunc func(context.Context, config.Config) error

type StreamD struct {
	UI ui.UI

	SaveConfigFunc SaveConfigFunc
	ConfigLock     sync.Mutex
	Config         config.Config

	CacheLock sync.Mutex
	Cache     *cache.Cache

	GitStorage *repository.GIT

	CancelGitSyncer context.CancelFunc
	GitSyncerMutex  sync.Mutex
	GitInitialized  bool

	StreamControllers StreamControllers

	Variables sync.Map

	OAuthListenPortsLocker sync.Mutex
	OAuthListenPorts       map[uint16]struct{}

	ControllersLocker sync.RWMutex

	StreamServerLocker sync.RWMutex
	StreamServer       *streamserver.StreamServer

	StreamStatusCache *memoize.MemoizeData

	EventBus eventbus.Bus
}

var _ api.StreamD = (*StreamD)(nil)

func New(
	config config.Config,
	ui ui.UI,
	saveCfgFunc SaveConfigFunc,
	b *belt.Belt,
) (*StreamD, error) {
	ctx := belt.CtxWithBelt(context.Background(), b)

	d := &StreamD{
		UI:                ui,
		SaveConfigFunc:    saveCfgFunc,
		Config:            config,
		Cache:             &cache.Cache{},
		OAuthListenPorts:  map[uint16]struct{}{},
		StreamStatusCache: memoize.NewMemoizeData(),
		EventBus:          eventbus.New(),
	}

	err := d.readCache(ctx)
	if err != nil {
		logger.FromBelt(b).Errorf("unable to read cache: %v", err)
	}

	return d, nil
}

func (d *StreamD) Run(ctx context.Context) (_ret error) { // TODO: delete the fetchConfig parameter
	logger.Debugf(ctx, "StreamD.Run()")
	defer func() { logger.Debugf(ctx, "/StreamD.Run(): %v", _ret) }()

	if !d.StreamServerLocker.TryLock() {
		return fmt.Errorf("somebody already locked StreamServerLocker")
	}
	defer d.StreamServerLocker.Unlock()

	if !d.ControllersLocker.TryLock() {
		return fmt.Errorf("somebody already locked ControllersLocker")
	}
	defer d.ControllersLocker.Unlock()

	d.UI.SetStatus("Initializing remote GIT storage...")
	err := d.FetchConfig(ctx)
	if err != nil {
		d.UI.DisplayError(fmt.Errorf("unable to initialize the GIT storage: %w", err))
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

	d.UI.SetStatus("Initializing UI...")
	return nil
}

func (d *StreamD) InitStreamServer(ctx context.Context) error {
	d.ControllersLocker.Lock()
	defer d.ControllersLocker.Unlock()
	return d.initStreamServer(ctx)
}

func (d *StreamD) initStreamServer(ctx context.Context) error {
	d.StreamServer = streamserver.New(&d.Config.StreamServer, newPlatformsControllerAdapter(d.StreamControllers))
	assert(d.StreamServer != nil)
	defer d.notifyAboutChange(ctx, events.StreamServersChange)
	return d.StreamServer.Init(ctx)
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
			logger.Errorf(ctx, "unable to write cache into '%s': %w", *d.Config.CachePath, err)
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

	allCategories, err := twitch.GetAllCategories(d.ctxForController(ctx))
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

	broadcasts, err := youtube.ListBroadcasts(d.ctxForController(ctx))
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
	defer d.notifyAboutChange(ctx, events.ConfigChange)
	err := d.SaveConfigFunc(ctx, d.Config)
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

func (d *StreamD) ResetCache(ctx context.Context) error {
	d.Cache.Twitch = cache.Twitch{}
	d.Cache.Youtube = cache.YouTube{}
	return nil
}

func (d *StreamD) GetConfig(ctx context.Context) (*config.Config, error) {
	return ptr(d.Config), nil
}

func (d *StreamD) SetConfig(ctx context.Context, cfg *config.Config) error {
	logger.Debugf(ctx, "SetConfig: %#+v", *cfg)
	d.Config = *cfg
	return nil
}

func (d *StreamD) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error) {
	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
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
	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	defer func() { logger.Debugf(ctx, "/StartStream(%s): %v", platID, _err) }()
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	defer func() {
		d.StreamStatusCache.InvalidateCache(ctx)
		if platID == youtube.ID {
			go func() {
				now := time.Now()
				time.Sleep(10 * time.Second)
				for time.Since(now) < 5*time.Minute {
					d.StreamStatusCache.InvalidateCache(ctx)
					time.Sleep(20 * time.Second)
				}
			}()
		}
	}()
	switch platID {
	case obs.ID:
		profile, err := streamcontrol.GetStreamProfile[obs.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into OBS profile: %w", err)
		}
		err = d.StreamControllers.OBS.StartStream(d.ctxForController(ctx), title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on OBS: %w", err)
		}
		return nil
	case twitch.ID:
		profile, err := streamcontrol.GetStreamProfile[twitch.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into Twitch profile: %w", err)
		}
		err = d.StreamControllers.Twitch.StartStream(d.ctxForController(ctx), title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on Twitch: %w", err)
		}
		return nil
	case youtube.ID:
		profile, err := streamcontrol.GetStreamProfile[youtube.StreamProfile](ctx, profile)
		if err != nil {
			return fmt.Errorf("unable to convert the profile into YouTube profile: %w", err)
		}
		err = d.StreamControllers.YouTube.StartStream(d.ctxForController(ctx), title, description, *profile, customArgs...)
		if err != nil {
			return fmt.Errorf("unable to start the stream on YouTube: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	defer d.StreamStatusCache.InvalidateCache(ctx)
	switch platID {
	case obs.ID:
		return d.StreamControllers.OBS.EndStream(d.ctxForController(ctx))
	case twitch.ID:
		return d.StreamControllers.Twitch.EndStream(d.ctxForController(ctx))
	case youtube.ID:
		return d.StreamControllers.YouTube.EndStream(d.ctxForController(ctx))
	default:
		return fmt.Errorf("unexpected platform ID '%s'", platID)
	}
}

func (d *StreamD) GetBackendData(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (any, error) {
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
		cacheDuration = 5 * time.Minute
	}
	return memoize.Memoize(d.StreamStatusCache, d.getStreamStatus, ctx, platID, cacheDuration)
}

func (d *StreamD) getStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	c, err := d.streamController(ctx, platID)
	if err != nil {
		return nil, err
	}

	if c == nil {
		return nil, fmt.Errorf("controller '%s' is not initialized", platID)
	}

	return c.GetStreamStatus(d.ctxForController(ctx))
}

func (d *StreamD) SetTitle(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string,
) error {
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	c, err := d.streamController(ctx, platID)
	if err != nil {
		return err
	}

	return c.SetTitle(d.ctxForController(ctx), title)
}

func (d *StreamD) SetDescription(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	description string,
) error {
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	c, err := d.streamController(ctx, platID)
	if err != nil {
		return err
	}

	return c.SetDescription(d.ctxForController(ctx), description)
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
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()
	c, err := d.streamController(d.ctxForController(ctx), platID)
	if err != nil {
		return err
	}

	return c.ApplyProfile(d.ctxForController(ctx), profile, customArgs...)
}

func (d *StreamD) UpdateStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	defer d.notifyAboutChange(ctx, events.StreamsChange)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()

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
	d.Variables.Store(key, value)
	return nil
}

func (d *StreamD) OBSGetSceneList(
	ctx context.Context,
) (*scenes.GetSceneListResponse, error) {
	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()

	obs := d.StreamControllers.OBS
	if obs == nil {
		return nil, fmt.Errorf("OBS is not initialized")
	}

	return obs.GetSceneList(d.ctxForController(ctx))
}

func (d *StreamD) OBSSetCurrentProgramScene(
	ctx context.Context,
	req *scenes.SetCurrentProgramSceneParams,
) error {
	defer d.notifyAboutChange(ctx, events.OBSCurrentProgramScene)

	d.ControllersLocker.RLock()
	defer d.ControllersLocker.RUnlock()

	obs := d.StreamControllers.OBS
	if obs == nil {
		return fmt.Errorf("OBS is not initialized")
	}

	return obs.SetCurrentProgramScene(d.ctxForController(ctx), req)
}

func (d *StreamD) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	err := d.UI.OnSubmittedOAuthCode(
		ctx,
		streamcontrol.PlatformName(req.GetPlatID()),
		req.GetCode(),
	)
	if err != nil {
		return nil, err
	}

	return &streamd_grpc.SubmitOAuthCodeReply{}, nil
}

func (d *StreamD) AddOAuthListenPort(port uint16) {
	logger.Default().Debugf("AddOAuthListenPort(%d)", port)
	defer logger.Default().Debugf("/AddOAuthListenPort(%d)", port)
	d.OAuthListenPortsLocker.Lock()
	defer d.OAuthListenPortsLocker.Unlock()
	d.OAuthListenPorts[port] = struct{}{}
}

func (d *StreamD) RemoveOAuthListenPort(port uint16) {
	logger.Default().Debugf("RemoveOAuthListenPort(%d)", port)
	defer logger.Default().Debugf("/RemoveOAuthListenPort(%d)", port)
	d.OAuthListenPortsLocker.Lock()
	defer d.OAuthListenPortsLocker.Unlock()
	delete(d.OAuthListenPorts, port)
}

func (d *StreamD) GetOAuthListenPorts() []uint16 {
	d.OAuthListenPortsLocker.Lock()
	defer d.OAuthListenPortsLocker.Unlock()

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

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	assert(d.StreamServer != nil)

	servers := d.StreamServer.ListServers(ctx)

	var result []api.StreamServer
	for _, src := range servers {
		result = append(result, api.StreamServer{
			Type:       src.Type(),
			ListenAddr: src.ListenAddr(),

			NumBytesConsumerWrote: src.NumBytesConsumerWrote(),
			NumBytesProducerRead:  src.NumBytesProducerRead(),
		})
	}

	return result, nil
}

func (d *StreamD) StartStreamServer(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
) error {
	logger.Debugf(ctx, "StartStreamServer")
	defer logger.Debugf(ctx, "/StartStreamServer")
	defer d.notifyAboutChange(ctx, events.StreamServersChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.StartServer(
		resetContextCancellers(ctx),
		serverType,
		listenAddr,
	)
	if err != nil {
		return fmt.Errorf("unable to start stream server: %w", err)
	}

	logger.Tracef(ctx, "new StreamServer.Servers config == %#+v", d.Config.StreamServer.Servers)
	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save config: %w", err)
	}

	return nil
}

func (d *StreamD) getStreamServerByListenAddr(
	ctx context.Context,
	listenAddr string,
) *types.PortServer {
	for _, server := range d.StreamServer.ListServers(ctx) {
		if server.ListenAddr() == listenAddr {
			return &server
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
	defer d.notifyAboutChange(ctx, events.StreamServersChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	srv := d.getStreamServerByListenAddr(ctx, listenAddr)
	if srv == nil {
		return fmt.Errorf("have not found any stream listeners at %s", listenAddr)
	}

	err := d.StreamServer.StopServer(ctx, *srv)
	if err != nil {
		return fmt.Errorf("unable to stop server %#+v: %w", *srv, err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (d *StreamD) AddIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "AddIncomingStream")
	defer logger.Debugf(ctx, "/AddIncomingStream")
	defer d.notifyAboutChange(ctx, events.IncomingStreamsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.AddIncomingStream(ctx, types.StreamID(streamID))
	if err != nil {
		return fmt.Errorf("unable to add an incoming stream: %w", err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (d *StreamD) RemoveIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	logger.Debugf(ctx, "RemoveIncomingStream")
	defer logger.Debugf(ctx, "/RemoveIncomingStream")
	defer d.notifyAboutChange(ctx, events.IncomingStreamsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.RemoveIncomingStream(ctx, types.StreamID(streamID))
	if err != nil {
		return fmt.Errorf("unable to remove an incoming stream: %w", err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (d *StreamD) ListIncomingStreams(
	ctx context.Context,
) ([]api.IncomingStream, error) {
	logger.Debugf(ctx, "ListIncomingStreams")
	defer logger.Debugf(ctx, "/ListIncomingStreams")

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	var result []api.IncomingStream
	for _, src := range d.StreamServer.ListIncomingStreams(ctx) {
		result = append(result, api.IncomingStream{
			StreamID: api.StreamID(src.StreamID),
		})
	}
	return result, nil
}

func (d *StreamD) ListStreamDestinations(
	ctx context.Context,
) ([]api.StreamDestination, error) {
	logger.Debugf(ctx, "ListStreamDestinations")
	defer logger.Debugf(ctx, "/ListStreamDestinations")

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	streamDestinations, err := d.StreamServer.ListStreamDestinations(ctx)
	if err != nil {
		return nil, err
	}
	c := make([]api.StreamDestination, 0, len(streamDestinations))
	for _, dst := range streamDestinations {
		c = append(c, api.StreamDestination{
			ID:  api.DestinationID(dst.ID),
			URL: dst.URL,
		})
	}
	return c, nil
}

func (d *StreamD) AddStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
) error {
	logger.Debugf(ctx, "AddStreamDestination")
	defer logger.Debugf(ctx, "/AddStreamDestination")
	defer d.notifyAboutChange(ctx, events.StreamDestinationsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.AddStreamDestination(
		resetContextCancellers(ctx),
		types.DestinationID(destinationID),
		url,
	)
	if err != nil {
		return fmt.Errorf("unable to add stream destination server: %w", err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func (d *StreamD) RemoveStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
) error {
	logger.Debugf(ctx, "RemoveStreamDestination")
	defer logger.Debugf(ctx, "/RemoveStreamDestination")
	defer d.notifyAboutChange(ctx, events.StreamDestinationsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.RemoveStreamDestination(ctx, types.DestinationID(destinationID))
	if err != nil {
		return fmt.Errorf("unable to remove stream destination server: %w", err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
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
		result = append(result, api.StreamForward{
			Enabled:       streamFwd.Enabled,
			StreamID:      api.StreamID(streamFwd.StreamID),
			DestinationID: api.DestinationID(streamFwd.DestinationID),
			NumBytesWrote: streamFwd.NumBytesWrote,
			NumBytesRead:  streamFwd.NumBytesRead,
		})
	}
	return result, nil
}

func (d *StreamD) ListStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	logger.Debugf(ctx, "ListStreamForwards")
	defer logger.Debugf(ctx, "/ListStreamForwards")

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	return d.listStreamForwards(ctx)
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
	defer d.notifyAboutChange(ctx, events.StreamForwardsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	_, err := d.StreamServer.AddStreamForward(
		resetContextCancellers(ctx),
		types.StreamID(streamID),
		types.DestinationID(destinationID),
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
	defer d.notifyAboutChange(ctx, events.StreamForwardsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	_, err := d.StreamServer.UpdateStreamForward(
		resetContextCancellers(ctx),
		types.StreamID(streamID),
		types.DestinationID(destinationID),
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
}

func (d *StreamD) RemoveStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
) error {
	logger.Debugf(ctx, "RemoveStreamForward")
	defer logger.Debugf(ctx, "/RemoveStreamForward")
	defer d.notifyAboutChange(ctx, events.StreamForwardsChange)

	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ing")
	d.StreamServerLocker.Lock()
	logger.Tracef(ctx, "d.StreamServerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "d.StreamServerLocker.Unlock()-ed")
	defer d.StreamServerLocker.Unlock()

	err := d.StreamServer.RemoveStreamForward(
		ctx,
		types.StreamID(streamID),
		types.DestinationID(destinationID),
	)
	if err != nil {
		return fmt.Errorf("unable to remove the stream forwarding: %w", err)
	}

	err = d.SaveConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to save the config: %w", err)
	}

	return nil
}

func resetContextCancellers(ctx context.Context) context.Context {
	return belt.CtxWithBelt(context.Background(), belt.CtxBelt(ctx))
}

func (d *StreamD) WaitForStreamPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	return streamserver.NewStreamPlayerStreamServer(d.StreamServer).WaitPublisher(ctx, streamID)
}

func (d *StreamD) GetStreamPortServers(
	ctx context.Context,
) ([]streamplayer.StreamPortServer, error) {
	return streamserver.NewStreamPlayerStreamServer(d.StreamServer).GetPortServers(ctx)
}

func (d *StreamD) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	defer d.notifyAboutChange(ctx, events.StreamPlayersChange)
	var result *multierror.Error
	result = multierror.Append(result, d.StreamServer.AddStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
	))
	result = multierror.Append(result, d.SaveConfig(ctx))
	return result.ErrorOrNil()
}

func (d *StreamD) UpdateStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	defer d.notifyAboutChange(ctx, events.StreamPlayersChange)
	var result *multierror.Error
	result = multierror.Append(result, d.StreamServer.UpdateStreamPlayer(
		ctx,
		streamID,
		playerType,
		disabled,
		streamPlaybackConfig,
	))
	result = multierror.Append(result, d.SaveConfig(ctx))
	return result.ErrorOrNil()
}

func (d *StreamD) RemoveStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	defer d.notifyAboutChange(ctx, events.StreamPlayersChange)
	var result *multierror.Error
	result = multierror.Append(result, d.StreamServer.RemoveStreamPlayer(
		ctx,
		streamID,
	))
	result = multierror.Append(result, d.SaveConfig(ctx))
	return result.ErrorOrNil()
}

func (d *StreamD) ListStreamPlayers(
	ctx context.Context,
) ([]api.StreamPlayer, error) {
	var result []api.StreamPlayer
	for streamID, streamCfg := range d.StreamServer.Config.Streams {
		playerCfg := streamCfg.Player
		if playerCfg == nil {
			continue
		}

		result = append(result, api.StreamPlayer{
			StreamID:             streamID,
			PlayerType:           playerCfg.Player,
			Disabled:             playerCfg.Disabled,
			StreamPlaybackConfig: playerCfg.StreamPlayback,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].StreamID < result[j].StreamID
	})
	return result, nil
}

func (d *StreamD) GetStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (*api.StreamPlayer, error) {
	streamCfg, ok := d.StreamServer.Config.Streams[streamID]
	if !ok {
		return nil, fmt.Errorf("no stream '%s'", streamID)
	}
	playerCfg := streamCfg.Player
	if playerCfg == nil {
		return nil, fmt.Errorf("no stream player defined for '%s'", streamID)
	}
	return &api.StreamPlayer{
		StreamID:             streamID,
		PlayerType:           playerCfg.Player,
		Disabled:             playerCfg.Disabled,
		StreamPlaybackConfig: playerCfg.StreamPlayback,
	}, nil
}

func (d *StreamD) StreamPlayerProcessTitle(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return "", fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.ProcessTitle(ctx)
}
func (d *StreamD) StreamPlayerOpenURL(
	ctx context.Context,
	streamID streamtypes.StreamID,
	link string,
) error {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.OpenURL(ctx, link)
}
func (d *StreamD) StreamPlayerGetLink(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return "", fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.GetLink(ctx)
}
func (d *StreamD) StreamPlayerEndChan(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (<-chan struct{}, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return nil, fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.EndChan(ctx)
}
func (d *StreamD) StreamPlayerIsEnded(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (bool, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return false, fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.IsEnded(ctx)
}
func (d *StreamD) StreamPlayerGetPosition(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return 0, fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.GetPosition(ctx)
}
func (d *StreamD) StreamPlayerGetLength(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return 0, fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.GetLength(ctx)
}
func (d *StreamD) StreamPlayerSetSpeed(
	ctx context.Context,
	streamID streamtypes.StreamID,
	speed float64,
) error {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.SetSpeed(ctx, speed)
}
func (d *StreamD) StreamPlayerSetPause(
	ctx context.Context,
	streamID streamtypes.StreamID,
	pause bool,
) error {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.SetPause(ctx, pause)
}
func (d *StreamD) StreamPlayerStop(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.Stop(ctx)
}
func (d *StreamD) StreamPlayerClose(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	streamPlayer := d.StreamServer.StreamPlayers.Get(streamID)
	if streamPlayer == nil || streamPlayer.Player == nil {
		return fmt.Errorf("there is no active player '%s'", streamID)
	}
	return streamPlayer.Player.Close(ctx)
}

func (d *StreamD) notifyAboutChange(
	_ context.Context,
	topic events.Event,
) {
	d.EventBus.Publish(topic)
}

func eventSubToChan[T any](
	ctx context.Context,
	d *StreamD,
	topic events.Event,
) (<-chan T, error) {
	r := make(chan T)
	callback := func() {
		var zeroValue T
		select {
		case r <- zeroValue:
		case <-time.After(time.Minute):
			logger.Errorf(ctx, "unable to notify about '%s': timeout", topic)
		}
	}
	err := d.EventBus.SubscribeAsync(topic, callback, true)
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe: %w", err)
	}

	go func() {
		<-ctx.Done()
		d.EventBus.Unsubscribe(topic, callback)
		close(r)
	}()
	return r, nil
}

func (d *StreamD) SubscribeToConfigChanges(
	ctx context.Context,
) (<-chan api.DiffConfig, error) {
	return eventSubToChan[api.DiffConfig](ctx, d, events.ConfigChange)
}

func (d *StreamD) SubscribeToStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffStreams, error) {
	return eventSubToChan[api.DiffStreams](ctx, d, events.StreamsChange)
}

func (d *StreamD) SubscribeToStreamServersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamServers, error) {
	return eventSubToChan[api.DiffStreamServers](ctx, d, events.StreamServersChange)
}

func (d *StreamD) SubscribeToStreamDestinationsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamDestinations, error) {
	return eventSubToChan[api.DiffStreamDestinations](ctx, d, events.StreamDestinationsChange)
}

func (d *StreamD) SubscribeToIncomingStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffIncomingStreams, error) {
	return eventSubToChan[api.DiffIncomingStreams](ctx, d, events.IncomingStreamsChange)
}

func (d *StreamD) SubscribeToStreamForwardsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamForwards, error) {
	return eventSubToChan[api.DiffStreamForwards](ctx, d, events.StreamForwardsChange)
}

func (d *StreamD) SubscribeToStreamPlayersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamPlayers, error) {
	return eventSubToChan[api.DiffStreamPlayers](ctx, d, events.StreamPlayersChange)
}
