package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/dustin/go-broadcast"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/immune-gmbh/attestation-sdk/pkg/lockmap"
	"github.com/immune-gmbh/attestation-sdk/pkg/objhash"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
)

type GRPCServer struct {
	streamd_grpc.StreamDServer
	StreamD                api.StreamD
	Cache                  map[objhash.ObjHash]any
	CacheMetaLock          sync.Mutex
	CacheLockMap           *lockmap.LockMap
	OAuthURLBroadcaster    broadcast.Broadcaster
	YouTubeStreamStartedAt time.Time
}

var _ streamd_grpc.StreamDServer = (*GRPCServer)(nil)

func NewGRPCServer(streamd api.StreamD) *GRPCServer {
	return &GRPCServer{
		StreamD:      streamd,
		Cache:        map[objhash.ObjHash]any{},
		CacheLockMap: lockmap.NewLockMap(),

		OAuthURLBroadcaster: broadcast.NewBroadcaster(10),
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

	grpc.CacheMetaLock.Unlock()
	logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
	if ok {
		if v, ok := cachedResult.(cacheItem); ok {
			cutoffTS := time.Now().Add(-cacheDuration)
			if cacheDuration > 0 && !v.SavedAt.Before(cutoffTS) {
				h.UserData = v
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
	err = multierror.Append(err, grpc.OAuthURLBroadcaster.Close())
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
) error {
	logger.Tracef(sender.Context(), "SubscribeToOAuthRequests()")
	defer logger.Tracef(sender.Context(), "/SubscribeToOAuthRequests()")

	oauthURLChan := make(chan any)
	grpc.OAuthURLBroadcaster.Register(oauthURLChan)
	defer grpc.OAuthURLBroadcaster.Unregister(oauthURLChan)

	for {
		select {
		case _oauthURL := <-oauthURLChan:
			oauthURL := _oauthURL.(string)
			err := sender.Send(&streamd_grpc.OAuthRequest{
				AuthURL: oauthURL,
			})
			if err != nil {
				return fmt.Errorf("unable to send the OAuth URL: %w", err)
			}
		case <-sender.Context().Done():
			return sender.Context().Err()
		}
	}
}

func (grpc *GRPCServer) OpenOAuthURL(authURL string) {
	grpc.OAuthURLBroadcaster.Submit(authURL)
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
	key := consts.VarKey(req.GetKey())
	b, err := grpc.StreamD.GetVariable(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("unable to get variable '%s': %w", key, err)
	}

	var hash []byte
	hashType := req.GetHashType()
	switch hashType {
	case streamd_grpc.HashType_HASH_SHA1:
		_hash := sha1.Sum(b)
		hash = _hash[:]
	default:
		return nil, fmt.Errorf("unexpected hash type: %v", hashType)
	}

	return &streamd_grpc.GetVariableHashReply{
		Key:      string(key),
		HashType: hashType,
		Hash:     hash,
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
