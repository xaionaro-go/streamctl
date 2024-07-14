package streamd

import (
	"context"
	"crypto"
	"fmt"
	"sort"
	"sync"

	"github.com/andreykaipov/goobs/api/requests/scenes"
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
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/ui"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamserver"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
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

	StreamServerLocker sync.Mutex
	StreamServer       *streamserver.StreamServer
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
		UI:               ui,
		SaveConfigFunc:   saveCfgFunc,
		Config:           config,
		Cache:            &cache.Cache{},
		OAuthListenPorts: map[uint16]struct{}{},
		StreamServer:     streamserver.New(),
	}

	err := d.readCache(ctx)
	if err != nil {
		logger.FromBelt(b).Errorf("unable to read cache: %v", err)
	}

	return d, nil
}

func (d *StreamD) Run(ctx context.Context) error {
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

	d.UI.SetStatus("Initializing UI...")
	return nil
}

func (d *StreamD) readCache(ctx context.Context) error {
	logger.Tracef(ctx, "readCache")
	defer logger.Tracef(ctx, "/readCache")

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
	defer func() { logger.Debugf(ctx, "/StartStream(%s): %v", platID, _err) }()
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

func serverTypeServer2API(t types.ServerType) api.StreamServerType {
	switch t {
	case types.ServerTypeUndefined:
		return api.StreamServerTypeUndefined
	case types.ServerTypeRTSP:
		return api.StreamServerTypeRTSP
	case types.ServerTypeRTMP:
		return api.StreamServerTypeRTMP
	default:
		panic(fmt.Errorf("unexpected server type: %v", t))
	}
}

func serverTypeAPI2Server(t api.StreamServerType) types.ServerType {
	switch t {
	case api.StreamServerTypeUndefined:
		return types.ServerTypeUndefined
	case api.StreamServerTypeRTSP:
		return types.ServerTypeRTSP
	case api.StreamServerTypeRTMP:
		return types.ServerTypeRTMP
	default:
		panic(fmt.Errorf("unexpected server type: %v", t))
	}
}

func (d *StreamD) ListStreamServers(
	ctx context.Context,
) ([]api.StreamServer, error) {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	servers := d.StreamServer.ListServers(ctx)

	var result []api.StreamServer
	for _, src := range servers {
		result = append(result, api.StreamServer{
			Type:       serverTypeServer2API(src.Type()),
			ListenAddr: src.ListenAddr(),
		})
	}

	return result, nil
}

func (d *StreamD) StartStreamServer(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.StreamServer.StartServer(
		ctx,
		serverTypeAPI2Server(serverType),
		listenAddr,
	)
}

func (d *StreamD) StopStreamServer(
	ctx context.Context,
	listenAddr string,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	for _, server := range d.StreamServer.ListServers(ctx) {
		if server.ListenAddr() == listenAddr {
			return d.StreamServer.StopServer(ctx, server)
		}
	}

	return fmt.Errorf("have not found any stream listeners at %s", listenAddr)
}

func (d *StreamD) ListIncomingStreams(
	ctx context.Context,
) ([]api.IncomingStream, error) {
	d.StreamServerLocker.Lock()
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
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	streamDestinations, err := d.StreamServer.ListStreamDestinations(ctx)
	if err != nil {
		return nil, err
	}
	c := make([]api.StreamDestination, 0, len(streamDestinations))
	for _, dst := range streamDestinations {
		c = append(c, api.StreamDestination{
			StreamID: api.StreamID(dst.StreamID),
			URL:      dst.URL,
		})
	}
	return c, nil
}

func (d *StreamD) AddStreamDestination(
	ctx context.Context,
	streamID api.StreamID,
	url string,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.StreamServer.AddStreamDestination(ctx, types.StreamID(streamID), url)
}

func (d *StreamD) RemoveStreamDestination(
	ctx context.Context,
	streamID api.StreamID,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.StreamServer.RemoveStreamDestination(ctx, types.StreamID(streamID))
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
			StreamIDSrc: api.StreamID(streamFwd.StreamIDSrc),
			StreamIDDst: api.StreamID(streamFwd.StreamIDDst),
		})
	}
	return result, nil
}

func (d *StreamD) ListStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.listStreamForwards(ctx)
}

func (d *StreamD) AddStreamForward(
	ctx context.Context,
	streamIDSrc api.StreamID,
	streamIDDst api.StreamID,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.StreamServer.AddStreamForward(ctx, types.StreamID(streamIDSrc), types.StreamID(streamIDDst))
}

func (d *StreamD) RemoveStreamForward(
	ctx context.Context,
	streamIDSrc api.StreamID,
	streamIDDst api.StreamID,
) error {
	d.StreamServerLocker.Lock()
	defer d.StreamServerLocker.Unlock()
	return d.StreamServer.RemoveStreamForward(ctx, types.StreamID(streamIDSrc), types.StreamID(streamIDDst))
}
