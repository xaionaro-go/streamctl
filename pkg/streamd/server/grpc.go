package server

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/immune-gmbh/attestation-sdk/pkg/lockmap"
	"github.com/immune-gmbh/attestation-sdk/pkg/objhash"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api/grpcconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type GRPCServer struct {
	streamd_grpc.UnimplementedStreamDServer
	StreamD                api.StreamD
	Cache                  map[objhash.ObjHash]any
	CacheMetaLock          sync.Mutex
	CacheLockMap           *lockmap.LockMap
	OAuthURLHandlerLocker  sync.Mutex
	OAuthURLHandlers       map[uint16]map[uuid.UUID]*OAuthURLHandler
	YouTubeStreamStartedAt time.Time

	UnansweredOAuthRequestsLocker sync.Mutex
	UnansweredOAuthRequests       map[streamcontrol.PlatformName]map[uint16]*streamd_grpc.OAuthRequest
}

type OAuthURLHandler struct {
	Sender   streamd_grpc.StreamD_SubscribeToOAuthRequestsServer
	CancelFn context.CancelFunc
}

var _ streamd_grpc.StreamDServer = (*GRPCServer)(nil)

func NewGRPCServer(streamd api.StreamD) *GRPCServer {
	return &GRPCServer{
		StreamD:      streamd,
		Cache:        map[objhash.ObjHash]any{},
		CacheLockMap: lockmap.NewLockMap(),

		OAuthURLHandlers:        map[uint16]map[uuid.UUID]*OAuthURLHandler{},
		UnansweredOAuthRequests: map[streamcontrol.PlatformName]map[uint16]*streamd_grpc.OAuthRequest{},
	}
}

const timeFormat = time.RFC3339

func memoize[REQ any, REPLY any, T func(context.Context, *REQ) (*REPLY, error)](
	grpc *GRPCServer,
	fn T,
	ctx context.Context,
	req *REQ,
	cacheDuration time.Duration,
) (_ret *REPLY, _err error) {
	logger.Debugf(ctx, "memoize %T", (*REQ)(nil))
	defer logger.Debugf(ctx, "/memoize %T", (*REQ)(nil))

	key, err := objhash.Build(fmt.Sprintf("%T", req), req)
	errmon.ObserveErrorCtx(ctx, err)
	if err != nil {
		return fn(ctx, req)
	}
	logger.Debugf(ctx, "cache key %X", key[:])

	grpc.CacheMetaLock.Lock()
	logger.Tracef(ctx, "grpc.CacheMetaLock.Lock()-ed")
	cache := grpc.Cache

	h := grpc.CacheLockMap.Lock(key)
	logger.Tracef(ctx, "grpc.CacheLockMap.Lock(%X)-ed", key[:])

	type cacheItem struct {
		Reply   *REPLY
		Error   error
		SavedAt time.Time
	}

	if h.UserData != nil {
		if v, ok := h.UserData.(cacheItem); ok {
			grpc.CacheMetaLock.Unlock()
			logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
			h.Unlock()
			logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])
			logger.Debugf(ctx, "re-using the value")
			return v.Reply, v.Error
		} else {
			logger.Errorf(ctx, "cache-failure: expected type %T, but got %T", (*cacheItem)(nil), h.UserData)
		}
	}

	//Jul 01 16:47:27 dx-landing start.sh[15530]: {"level":"error","ts":1719852447.1707313,"caller":"server/grpc.go:91","msg":"cache-failure: expected type
	//server.cacheItem[github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetStreamStatusRequest,github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetStreamStatusReply,func(context.Context, *github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetStreamStatusRequest) (*github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetStreamStatusReply, error)], but got
	//server.cacheItem[github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetBackendInfoRequest,github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetBackendInfoReply,func(context.Context, *github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetBackendInfoRequest) (*github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc.GetBackendInfoReply, error)]"}

	cachedResult, ok := cache[key]

	if ok {
		if v, ok := cachedResult.(cacheItem); ok {
			cutoffTS := time.Now().Add(-cacheDuration)
			if cacheDuration > 0 && !v.SavedAt.Before(cutoffTS) {
				grpc.CacheMetaLock.Unlock()
				logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
				h.UserData = nil
				h.Unlock()
				logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])
				logger.Debugf(ctx, "using the cached value")
				return v.Reply, v.Error
			}
			logger.Debugf(ctx, "the cached value expired: %s < %s", v.SavedAt.Format(timeFormat), cutoffTS.Format(timeFormat))
			delete(cache, key)
		} else {
			logger.Errorf(ctx, "cache-failure: expected type %T, but got %T", (*cacheItem)(nil), cachedResult)
		}
	}
	grpc.CacheMetaLock.Unlock()
	logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")

	var ts time.Time
	defer func() {
		cacheItem := cacheItem{
			Reply:   _ret,
			Error:   _err,
			SavedAt: ts,
		}
		h.UserData = cacheItem
		h.Unlock()
		logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])

		grpc.CacheMetaLock.Lock()
		logger.Tracef(ctx, "grpc.CacheMetaLock.Lock()-ed")
		defer logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
		defer grpc.CacheMetaLock.Unlock()
		cache[key] = cacheItem
	}()

	logger.Tracef(ctx, "no cache")
	ts = time.Now()
	return fn(ctx, req)
}

func (grpc *GRPCServer) Close() error {
	err := &multierror.Error{}
	grpc.OAuthURLHandlerLocker.Lock()
	grpc.OAuthURLHandlerLocker.Unlock()
	for listenPort, sender := range grpc.OAuthURLHandlers {
		_ = sender // TODO: invent sender.Close()
		delete(grpc.OAuthURLHandlers, listenPort)
	}
	return err.ErrorOrNil()
}

func (grpc *GRPCServer) invalidateCache(ctx context.Context) {
	logger.Debugf(ctx, "invalidateCache()")
	defer logger.Debugf(ctx, "/invalidateCache()")

	grpc.CacheMetaLock.Lock()
	defer grpc.CacheMetaLock.Unlock()

	grpc.CacheLockMap = lockmap.NewLockMap()
	grpc.Cache = map[objhash.ObjHash]any{}
}

func (grpc *GRPCServer) GetConfig(
	ctx context.Context,
	req *streamd_grpc.GetConfigRequest,
) (*streamd_grpc.GetConfigReply, error) {
	return memoize(grpc, grpc.getConfig, ctx, req, time.Second)
}

func (grpc *GRPCServer) getConfig(
	ctx context.Context,
	req *streamd_grpc.GetConfigRequest,
) (*streamd_grpc.GetConfigReply, error) {
	cfg, err := grpc.StreamD.GetConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the config: %w", err)
	}
	var buf bytes.Buffer
	_, err = cfg.WriteTo(&buf)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the config: %w", err)
	}
	return &streamd_grpc.GetConfigReply{
		Config: buf.String(),
	}, nil
}

func (grpc *GRPCServer) SetConfig(
	ctx context.Context,
	req *streamd_grpc.SetConfigRequest,
) (*streamd_grpc.SetConfigReply, error) {

	logger.Debugf(ctx, "received SetConfig: %s", req.Config)

	var result config.Config
	_, err := result.Read([]byte(req.Config))
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the config: %w", err)
	}

	err = grpc.StreamD.SetConfig(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("unable to set the config: %w", err)
	}

	grpc.invalidateCache(ctx)
	return &streamd_grpc.SetConfigReply{}, nil
}

func (grpc *GRPCServer) SaveConfig(
	ctx context.Context,
	req *streamd_grpc.SaveConfigRequest,
) (*streamd_grpc.SaveConfigReply, error) {
	err := grpc.StreamD.SaveConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to save the config: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.SaveConfigReply{}, nil
}

func (grpc *GRPCServer) ResetCache(
	ctx context.Context,
	req *streamd_grpc.ResetCacheRequest,
) (*streamd_grpc.ResetCacheReply, error) {
	err := grpc.StreamD.ResetCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to reset the cache: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.ResetCacheReply{}, nil
}

func (grpc *GRPCServer) InitCache(
	ctx context.Context,
	req *streamd_grpc.InitCacheRequest,
) (*streamd_grpc.InitCacheReply, error) {
	err := grpc.StreamD.InitCache(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to init the cache: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.InitCacheReply{}, nil
}

func (grpc *GRPCServer) StartStream(
	ctx context.Context,
	req *streamd_grpc.StartStreamRequest,
) (*streamd_grpc.StartStreamReply, error) {
	logger.Debugf(ctx, "grpc:StartStream: raw profile: %#+v", req.Profile)

	var profile streamcontrol.AbstractStreamProfile
	var err error
	platID := streamcontrol.PlatformName(req.GetPlatID())
	switch platID {
	case obs.ID:
		profile = &obs.StreamProfile{}
	case twitch.ID:
		profile = &twitch.StreamProfile{}
	case youtube.ID:
		profile = &youtube.StreamProfile{}
	default:
		return nil, fmt.Errorf("unexpected platform ID: '%s'", platID)
	}
	err = yaml.Unmarshal([]byte(req.GetProfile()), profile)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the profile: %w", err)
	}

	logger.Debugf(ctx, "grpc:StartStream: parsed: %#+v", profile)

	err = grpc.StreamD.StartStream(
		ctx,
		platID,
		req.GetTitle(),
		req.GetDescription(),
		profile,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to start the stream: %w", err)
	}

	grpc.CacheMetaLock.Lock()
	grpc.YouTubeStreamStartedAt = time.Now()
	grpc.CacheMetaLock.Unlock()
	grpc.invalidateCache(ctx)
	return &streamd_grpc.StartStreamReply{}, nil
}

func (grpc *GRPCServer) EndStream(
	ctx context.Context,
	req *streamd_grpc.EndStreamRequest,
) (*streamd_grpc.EndStreamReply, error) {
	err := grpc.StreamD.EndStream(ctx, streamcontrol.PlatformName(req.GetPlatID()))
	if err != nil {
		return nil, fmt.Errorf("unable to end the stream: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.EndStreamReply{}, nil
}

func (grpc *GRPCServer) GetBackendInfo(
	ctx context.Context,
	req *streamd_grpc.GetBackendInfoRequest,
) (*streamd_grpc.GetBackendInfoReply, error) {
	return memoize(grpc, grpc.getBackendInfo, ctx, req, time.Second)
}

func (grpc *GRPCServer) getBackendInfo(
	ctx context.Context,
	req *streamd_grpc.GetBackendInfoRequest,
) (*streamd_grpc.GetBackendInfoReply, error) {
	platID := streamcontrol.PlatformName(req.GetPlatID())
	isEnabled, err := grpc.StreamD.IsBackendEnabled(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to check if the backend is enabled: %w", err)
	}
	data, err := grpc.StreamD.GetBackendData(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get the backend info: %w", err)
	}
	dataSerialized, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize the backend info: %w", err)
	}

	return &streamd_grpc.GetBackendInfoReply{
		IsInitialized: isEnabled,
		Data:          string(dataSerialized),
	}, nil
}

func (grpc *GRPCServer) Restart(
	ctx context.Context,
	req *streamd_grpc.RestartRequest,
) (*streamd_grpc.RestartReply, error) {
	err := grpc.StreamD.Restart(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to restart: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.RestartReply{}, nil
}

func (grpc *GRPCServer) OBSOLETE_FetchConfig(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_FetchConfigRequest,
) (*streamd_grpc.OBSOLETE_FetchConfigReply, error) {
	err := grpc.StreamD.FetchConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch the config: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.OBSOLETE_FetchConfigReply{}, nil
}

func (grpc *GRPCServer) OBSOLETE_GitInfo(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_GetGitInfoRequest,
) (*streamd_grpc.OBSOLETE_GetGitInfoReply, error) {
	return memoize(grpc, grpc._OBSOLETE_GitInfo, ctx, req, time.Minute)
}

func (grpc *GRPCServer) _OBSOLETE_GitInfo(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_GetGitInfoRequest,
) (*streamd_grpc.OBSOLETE_GetGitInfoReply, error) {
	isEnabled, err := grpc.StreamD.OBSOLETE_IsGITInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the git info: %w", err)
	}
	return &streamd_grpc.OBSOLETE_GetGitInfoReply{
		IsInitialized: isEnabled,
	}, nil
}

func (grpc *GRPCServer) OBSOLETE_GitRelogin(
	ctx context.Context,
	req *streamd_grpc.OBSOLETE_GitReloginRequest,
) (*streamd_grpc.OBSOLETE_GitReloginReply, error) {
	err := grpc.StreamD.OBSOLETE_GitRelogin(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to relogin: %w", err)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.OBSOLETE_GitReloginReply{}, nil
}

func (grpc *GRPCServer) GetStreamStatus(
	ctx context.Context,
	req *streamd_grpc.GetStreamStatusRequest,
) (*streamd_grpc.GetStreamStatusReply, error) {
	logger.Tracef(ctx, "GetStreamStatus()")
	defer logger.Tracef(ctx, "/GetStreamStatus()")

	cacheDuration := time.Minute
	switch streamcontrol.PlatformName(req.PlatID) {
	case obs.ID:
		cacheDuration = 10 * time.Second
	case youtube.ID:
		if grpc.YouTubeStreamStartedAt.After(time.Now().Add(-time.Minute)) {
			cacheDuration = 15 * time.Second
		} else {
			cacheDuration = 10 * time.Minute
		}
	case twitch.ID:
		cacheDuration = time.Minute
	}

	if req.NoCache {
		cacheDuration = 0
	}

	return memoize(grpc, grpc.getStreamStatus, ctx, req, cacheDuration)
}

func (grpc *GRPCServer) getStreamStatus(
	ctx context.Context,
	req *streamd_grpc.GetStreamStatusRequest,
) (*streamd_grpc.GetStreamStatusReply, error) {
	logger.Tracef(ctx, "getStreamStatus()")
	defer logger.Tracef(ctx, "/getStreamStatus()")

	platID := streamcontrol.PlatformName(req.GetPlatID())
	streamStatus, err := grpc.StreamD.GetStreamStatus(ctx, platID)
	if err != nil {
		return nil, fmt.Errorf("unable to get the stream status of '%s': %w", platID, err)
	}

	var startedAt *int64
	if streamStatus.StartedAt != nil {
		startedAt = ptr(streamStatus.StartedAt.UnixNano())
	}

	var customData string
	if streamStatus.CustomData != nil {
		customDataSerialized, err := json.Marshal(streamStatus.CustomData)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize the custom data: %w", err)
		}
		customData = string(customDataSerialized)
	}

	return &streamd_grpc.GetStreamStatusReply{
		IsActive:   streamStatus.IsActive,
		StartedAt:  startedAt,
		CustomData: customData,
	}, nil
}

func (grpc *GRPCServer) SubscribeToOAuthRequests(
	req *streamd_grpc.SubscribeToOAuthRequestsRequest,
	sender streamd_grpc.StreamD_SubscribeToOAuthRequestsServer,
) (_ret error) {
	ctx, cancelFn := context.WithCancel(sender.Context())
	_uuid, err := uuid.NewRandom()
	if err != nil {
		logger.Errorf(ctx, "unable to generate an UUID: %v", err)
	}

	logger.Tracef(sender.Context(), "SubscribeToOAuthRequests(): UUID:%s", _uuid)
	defer func() { logger.Tracef(sender.Context(), "/SubscribeToOAuthRequests(): UUID:%s: %v", _uuid, _ret) }()

	listenPort := uint16(req.ListenPort)
	streamD, _ := grpc.StreamD.(*streamd.StreamD)

	grpc.OAuthURLHandlerLocker.Lock()
	logger.Tracef(sender.Context(), "grpc.OAuthURLHandlerLocker.Lock()-ed")
	m := grpc.OAuthURLHandlers[listenPort]
	if m == nil {
		m = map[uuid.UUID]*OAuthURLHandler{}
	}
	m[_uuid] = &OAuthURLHandler{
		Sender:   sender,
		CancelFn: cancelFn,
	}
	grpc.OAuthURLHandlers[listenPort] = m // unnecessary, but feels safer
	grpc.OAuthURLHandlerLocker.Unlock()
	logger.Tracef(sender.Context(), "grpc.OAuthURLHandlerLocker.Unlock()-ed")

	var unansweredRequests []*streamd_grpc.OAuthRequest
	grpc.UnansweredOAuthRequestsLocker.Lock()
	for _, m := range grpc.UnansweredOAuthRequests {
		req := m[listenPort]
		if req == nil {
			continue
		}
		unansweredRequests = append(unansweredRequests, req)
	}
	grpc.UnansweredOAuthRequestsLocker.Unlock()

	for _, req := range unansweredRequests {
		logger.Tracef(ctx, "re-sending an unanswered request to a new client: %#+v", *req)
		err := sender.Send(req)
		errmon.ObserveErrorCtx(ctx, err)
	}

	if streamD != nil {
		streamD.AddOAuthListenPort(listenPort)
	}

	logger.Tracef(ctx, "waiting for the subscription to be cancelled")
	<-ctx.Done()
	grpc.OAuthURLHandlerLocker.Lock()
	defer grpc.OAuthURLHandlerLocker.Unlock()
	delete(grpc.OAuthURLHandlers[listenPort], _uuid)
	if len(grpc.OAuthURLHandlers[listenPort]) == 0 {
		delete(grpc.OAuthURLHandlers, listenPort)
		if streamD != nil {
			streamD.RemoveOAuthListenPort(listenPort)
		}
	}

	return nil
}

type ErrNoOAuthHandlerForPort struct {
	Port uint16
}

func (err ErrNoOAuthHandlerForPort) Error() string {
	return fmt.Sprintf("no handler for port %d", err.Port)
}

func (grpc *GRPCServer) OpenOAuthURL(
	ctx context.Context,
	listenPort uint16,
	platID streamcontrol.PlatformName,
	authURL string,
) (_ret error) {
	logger.Tracef(ctx, "OpenOAuthURL()")
	defer func() { logger.Tracef(ctx, "/OpenOAuthURL(): %v", _ret) }()

	grpc.OAuthURLHandlerLocker.Lock()
	logger.Tracef(ctx, "grpc.OAuthURLHandlerLocker.Lock()-ed")
	defer logger.Tracef(ctx, "grpc.OAuthURLHandlerLocker.Unlock()-ed")
	defer grpc.OAuthURLHandlerLocker.Unlock()

	handlers := grpc.OAuthURLHandlers[listenPort]
	if handlers == nil {
		return ErrNoOAuthHandlerForPort{
			Port: listenPort,
		}
	}
	req := streamd_grpc.OAuthRequest{
		PlatID:  string(platID),
		AuthURL: authURL,
	}
	grpc.UnansweredOAuthRequestsLocker.Lock()
	if grpc.UnansweredOAuthRequests[platID] == nil {
		grpc.UnansweredOAuthRequests[platID] = map[uint16]*streamd_grpc.OAuthRequest{}
	}
	grpc.UnansweredOAuthRequests[platID][listenPort] = &req
	grpc.UnansweredOAuthRequestsLocker.Unlock()
	logger.Tracef(ctx, "OpenOAuthURL() sending %#+v", req)
	var resultErr *multierror.Error
	for _, handler := range handlers {
		err := handler.Sender.Send(&req)
		if err != nil {
			err = multierror.Append(resultErr, fmt.Errorf("unable to send oauth request: %w", err))
		}
	}
	return resultErr.ErrorOrNil()
}

func (grpc *GRPCServer) GetVariable(
	ctx context.Context,
	req *streamd_grpc.GetVariableRequest,
) (*streamd_grpc.GetVariableReply, error) {
	key := consts.VarKey(req.GetKey())
	b, err := grpc.StreamD.GetVariable(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("unable to get variable '%s': %w", key, err)
	}

	return &streamd_grpc.GetVariableReply{
		Key:   string(key),
		Value: b,
	}, nil
}

func (grpc *GRPCServer) GetVariableHash(
	ctx context.Context,
	req *streamd_grpc.GetVariableHashRequest,
) (*streamd_grpc.GetVariableHashReply, error) {
	var hashType crypto.Hash
	hashTypeIn := req.GetHashType()
	switch hashTypeIn {
	case streamd_grpc.HashType_HASH_SHA1:
		hashType = crypto.SHA1
	default:
		return nil, fmt.Errorf("unexpected hash type: %v", hashTypeIn)
	}

	key := consts.VarKey(req.GetKey())
	b, err := grpc.StreamD.GetVariableHash(ctx, key, hashType)
	if err != nil {
		return nil, fmt.Errorf("unable to get variable '%s': %w", key, err)
	}

	return &streamd_grpc.GetVariableHashReply{
		Key:      string(key),
		HashType: hashTypeIn,
		Hash:     b,
	}, nil
}

func (grpc *GRPCServer) SetVariable(
	ctx context.Context,
	req *streamd_grpc.SetVariableRequest,
) (*streamd_grpc.SetVariableReply, error) {
	key := consts.VarKey(req.GetKey())
	err := grpc.StreamD.SetVariable(ctx, key, req.GetValue())
	if err != nil {
		return nil, fmt.Errorf("unable to set variable '%s': %w", key, err)
	}

	return &streamd_grpc.SetVariableReply{}, nil
}

func (grpc *GRPCServer) OBSGetSceneList(
	ctx context.Context,
	req *streamd_grpc.OBSGetSceneListRequest,
) (*streamd_grpc.OBSGetSceneListReply, error) {
	resp, err := grpc.StreamD.OBSGetSceneList(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of scenes: %w", err)
	}

	result := &streamd_grpc.OBSGetSceneListReply{
		CurrentPreviewSceneName: resp.CurrentPreviewSceneName,
		CurrentPreviewSceneUUID: resp.CurrentPreviewSceneUuid,
		CurrentProgramSceneName: resp.CurrentProgramSceneName,
		CurrentProgramSceneUUID: resp.CurrentProgramSceneUuid,
	}
	for _, scene := range resp.Scenes {
		result.Scenes = append(result.Scenes, &streamd_grpc.OBSScene{
			Uuid:  scene.SceneUuid,
			Index: int32(scene.SceneIndex),
			Name:  scene.SceneName,
		})
	}
	return result, nil
}

func (grpc *GRPCServer) OBSSetCurrentProgramScene(
	ctx context.Context,
	req *streamd_grpc.OBSSetCurrentProgramSceneRequest,
) (*streamd_grpc.OBSSetCurrentProgramSceneReply, error) {
	params := &scenes.SetCurrentProgramSceneParams{}
	switch sceneID := req.GetOBSSceneID().(type) {
	case *streamd_grpc.OBSSetCurrentProgramSceneRequest_SceneName:
		params.SceneName = &sceneID.SceneName
	case *streamd_grpc.OBSSetCurrentProgramSceneRequest_SceneUUID:
		params.SceneUuid = &sceneID.SceneUUID
	default:
		return nil, fmt.Errorf("unexpected type: %T", sceneID)
	}

	err := grpc.StreamD.OBSSetCurrentProgramScene(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("unable to set the scene: %w", err)
	}
	return &streamd_grpc.OBSSetCurrentProgramSceneReply{}, nil
}

func (grpc *GRPCServer) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	grpc.UnansweredOAuthRequestsLocker.Lock()
	delete(grpc.UnansweredOAuthRequests, streamcontrol.PlatformName(req.PlatID))
	grpc.UnansweredOAuthRequestsLocker.Unlock()

	_, err := grpc.StreamD.SubmitOAuthCode(ctx, req)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.SubmitOAuthCodeReply{}, nil
}

func (grpc *GRPCServer) ListStreamServers(
	ctx context.Context,
	_ *streamd_grpc.ListStreamServersRequest,
) (*streamd_grpc.ListStreamServersReply, error) {
	servers, err := grpc.StreamD.ListStreamServers(ctx)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.StreamServer
	for _, srv := range servers {
		t, err := grpcconv.StreamServerTypeGo2GRPC(srv.Type)
		if err != nil {
			return nil, fmt.Errorf("unable to convert the server type value: %w", err)
		}

		result = append(result, &streamd_grpc.StreamServer{
			ServerType: t,
			ListenAddr: srv.ListenAddr,
		})
	}
	return &streamd_grpc.ListStreamServersReply{
		StreamServers: result,
	}, nil
}

func (grpc *GRPCServer) StartStreamServer(
	ctx context.Context,
	req *streamd_grpc.StartStreamServerRequest,
) (*streamd_grpc.StartStreamServerReply, error) {
	t, err := grpcconv.StreamServerTypeGRPC2Go(req.GetConfig().GetServerType())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the server type value: %w", err)
	}

	err = grpc.StreamD.StartStreamServer(
		ctx,
		t,
		req.GetConfig().GetListenAddr(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StartStreamServerReply{}, nil
}

func (grpc *GRPCServer) StopStreamServer(
	ctx context.Context,
	req *streamd_grpc.StopStreamServerRequest,
) (*streamd_grpc.StopStreamServerReply, error) {
	err := grpc.StreamD.StopStreamServer(
		ctx,
		req.GetListenAddr(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StopStreamServerReply{}, nil
}

func (grpc *GRPCServer) ListStreamDestinations(
	ctx context.Context,
	req *streamd_grpc.ListStreamDestinationsRequest,
) (*streamd_grpc.ListStreamDestinationsReply, error) {
	dsts, err := grpc.StreamD.ListStreamDestinations(
		ctx,
	)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.StreamDestination
	for _, dst := range dsts {
		result = append(result, &streamd_grpc.StreamDestination{
			StreamID: string(dst.StreamID),
			Url:      dst.URL,
		})
	}
	return &streamd_grpc.ListStreamDestinationsReply{
		StreamDestinations: result,
	}, nil
}

func (grpc *GRPCServer) AddStreamDestination(
	ctx context.Context,
	req *streamd_grpc.AddStreamDestinationRequest,
) (*streamd_grpc.AddStreamDestinationReply, error) {
	err := grpc.StreamD.AddStreamDestination(
		ctx,
		api.StreamID(req.GetConfig().GetStreamID()),
		req.GetConfig().GetUrl(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamDestinationReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamDestination(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamDestinationRequest,
) (*streamd_grpc.RemoveStreamDestinationReply, error) {
	err := grpc.StreamD.RemoveStreamDestination(
		ctx,
		api.StreamID(req.GetStreamID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamDestinationReply{}, nil
}

func (grpc *GRPCServer) ListIncomingStreams(
	ctx context.Context,
	req *streamd_grpc.ListIncomingStreamsRequest,
) (*streamd_grpc.ListIncomingStreamsReply, error) {
	inStreams, err := grpc.StreamD.ListIncomingStreams(
		ctx,
	)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.IncomingStream
	for _, s := range inStreams {
		result = append(result, &streamd_grpc.IncomingStream{
			StreamID: string(s.StreamID),
		})
	}
	return &streamd_grpc.ListIncomingStreamsReply{
		IncomingStreams: result,
	}, nil
}

func (grpc *GRPCServer) ListStreamForwards(
	ctx context.Context,
	req *streamd_grpc.ListStreamForwardsRequest,
) (*streamd_grpc.ListStreamForwardsReply, error) {
	streamFwds, err := grpc.StreamD.ListStreamForwards(ctx)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.StreamForward
	for _, s := range streamFwds {
		result = append(result, &streamd_grpc.StreamForward{
			StreamIDSrc: string(s.StreamIDSrc),
			StreamIDDst: string(s.StreamIDDst),
		})
	}
	return &streamd_grpc.ListStreamForwardsReply{
		StreamForwards: result,
	}, nil
}

func (grpc *GRPCServer) AddStreamForward(
	ctx context.Context,
	req *streamd_grpc.AddStreamForwardRequest,
) (*streamd_grpc.AddStreamForwardReply, error) {
	err := grpc.StreamD.AddStreamForward(
		ctx,
		api.StreamID(req.GetConfig().GetStreamIDSrc()),
		api.StreamID(req.GetConfig().GetStreamIDDst()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamForwardReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamForward(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamForwardRequest,
) (*streamd_grpc.RemoveStreamForwardReply, error) {
	err := grpc.StreamD.RemoveStreamForward(
		ctx,
		api.StreamID(req.GetConfig().GetStreamIDSrc()),
		api.StreamID(req.GetConfig().GetStreamIDDst()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamForwardReply{}, nil
}
