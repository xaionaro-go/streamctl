package server

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/player/protobuf/go/player_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streamd/platcollection"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"google.golang.org/grpc"
)

type GRPCServer struct {
	streamd_grpc.UnimplementedStreamDServer
	StreamD               api.StreamD
	MemoizeDataValue      *memoize.MemoizeData
	OAuthURLHandlerLocker sync.Mutex
	OAuthURLHandlers      map[uint16]map[uuid.UUID]*OAuthURLHandler

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
		StreamD:          streamd,
		MemoizeDataValue: memoize.NewMemoizeData(),

		OAuthURLHandlers:        map[uint16]map[uuid.UUID]*OAuthURLHandler{},
		UnansweredOAuthRequests: map[streamcontrol.PlatformName]map[uint16]*streamd_grpc.OAuthRequest{},
	}
}

func (grpc *GRPCServer) MemoizeData() *memoize.MemoizeData {
	return grpc.MemoizeDataValue
}

func (grpc *GRPCServer) Ping(
	ctx context.Context,
	req *streamd_grpc.PingRequest,
) (*streamd_grpc.PingReply, error) {
	var payload strings.Builder
	extraSize := req.GetRequestExtraPayloadSize()
	totalSize := len(req.GetPayloadToReturn()) + int(extraSize)
	if totalSize > 65535 {
		return nil, fmt.Errorf("requested a too big payload")
	}
	payload.WriteString(req.GetPayloadToReturn())
	payload.WriteString(strings.Repeat("0", int(extraSize)))
	return &streamd_grpc.PingReply{
		Payload: payload.String(),
	}, nil
}

func (grpc *GRPCServer) Close() error {
	err := &multierror.Error{}
	grpc.OAuthURLHandlerLocker.Lock()
	hs := grpc.OAuthURLHandlers
	grpc.OAuthURLHandlerLocker.Unlock()
	for listenPort, sender := range hs {
		_ = sender // TODO: invent sender.Close()
		delete(grpc.OAuthURLHandlers, listenPort)
	}
	return err.ErrorOrNil()
}

func (grpc *GRPCServer) invalidateCache(ctx context.Context) {
	grpc.MemoizeDataValue.InvalidateCache(ctx)
}

func (grpc *GRPCServer) SetLoggingLevel(
	ctx context.Context,
	req *streamd_grpc.SetLoggingLevelRequest,
) (*streamd_grpc.SetLoggingLevelReply, error) {
	err := grpc.StreamD.SetLoggingLevel(ctx, goconv.LoggingLevelGRPC2Go(req.GetLoggingLevel()))
	if err != nil {
		return nil, fmt.Errorf("unable to set the logging level: %w", err)
	}
	return &streamd_grpc.SetLoggingLevelReply{}, nil
}

func (grpc *GRPCServer) GetLoggingLevel(
	ctx context.Context,
	req *streamd_grpc.GetLoggingLevelRequest,
) (*streamd_grpc.GetLoggingLevelReply, error) {
	level, err := grpc.StreamD.GetLoggingLevel(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to query the logging level: %w", err)
	}
	return &streamd_grpc.GetLoggingLevelReply{
		LoggingLevel: goconv.LoggingLevelGo2GRPC(level),
	}, nil
}

func (grpc *GRPCServer) GetConfig(
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

func (grpc *GRPCServer) IsBackendEnabled(
	ctx context.Context,
	req *streamd_grpc.IsBackendEnabledRequest,
) (*streamd_grpc.IsBackendEnabledReply, error) {
	enabled, err := grpc.StreamD.IsBackendEnabled(ctx, streamcontrol.PlatformName(req.GetPlatID()))
	if err != nil {
		return nil, fmt.Errorf("unable to check if backend '%s' is enabled: %w", req.GetPlatID(), err)
	}
	return &streamd_grpc.IsBackendEnabledReply{
		IsInitialized: enabled,
	}, nil
}

func (grpc *GRPCServer) GetBackendInfo(
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

func (grpc *GRPCServer) SetTitle(
	ctx context.Context,
	req *streamd_grpc.SetTitleRequest,
) (*streamd_grpc.SetTitleReply, error) {
	err := grpc.StreamD.SetTitle(
		ctx,
		streamcontrol.PlatformName(req.GetPlatID()),
		req.GetTitle(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.SetTitleReply{}, nil
}

func (grpc *GRPCServer) SetDescription(
	ctx context.Context,
	req *streamd_grpc.SetDescriptionRequest,
) (*streamd_grpc.SetDescriptionReply, error) {
	err := grpc.StreamD.SetDescription(
		ctx,
		streamcontrol.PlatformName(req.GetPlatID()),
		req.GetDescription(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.SetDescriptionReply{}, nil
}

func (grpc *GRPCServer) ApplyProfile(
	ctx context.Context,
	req *streamd_grpc.ApplyProfileRequest,
) (*streamd_grpc.ApplyProfileReply, error) {
	platID := streamcontrol.PlatformName(req.GetPlatID())
	profile, err := platcollection.NewStreamProfile(platID)
	if err != nil {
		return nil, fmt.Errorf("unable to build a profile for platform '%s': %w", platID, err)
	}
	err = yaml.Unmarshal([]byte(req.GetProfile()), profile)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the profile '%s': %w", req.GetProfile(), err)
	}

	logger.Debugf(ctx, "unserialized profile: %#+v", profile)

	err = grpc.StreamD.ApplyProfile(
		ctx,
		streamcontrol.PlatformName(req.GetPlatID()),
		profile,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to apply the profile %#+v: %w", profile, err)
	}
	return &streamd_grpc.ApplyProfileReply{}, nil
}

func (grpc *GRPCServer) UpdateStream(
	ctx context.Context,
	req *streamd_grpc.UpdateStreamRequest,
) (*streamd_grpc.UpdateStreamReply, error) {
	platID := streamcontrol.PlatformName(req.GetPlatID())
	profile, err := platcollection.NewStreamProfile(platID)
	if err != nil {
		return nil, fmt.Errorf("unable to build a profile for platform '%s': %w", platID, err)
	}
	err = yaml.Unmarshal([]byte(req.GetProfile()), profile)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the profile '%s': %w", req.GetProfile(), err)
	}

	logger.Debugf(ctx, "unserialized profile: %#+v", profile)

	err = grpc.StreamD.UpdateStream(
		ctx,
		platID,
		req.GetTitle(),
		req.GetDescription(),
		profile,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to update stream details: %w", err)
	}
	return &streamd_grpc.UpdateStreamReply{}, nil
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

	if req.NoCache {
		ctx = memoize.SetNoCache(ctx, true)
	}

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

func (grpc *GRPCServer) OpenBrowser(
	ctx context.Context,
	url string,
) (_ret error) {
	logger.Debugf(ctx, "OpenBrowser(ctx, '%s')", url)
	defer func() {
		logger.Debugf(ctx, "/OpenBrowser(ctx, '%s'): %v", url, _ret)
	}()

	// TODO: Stop abusing the function for OAuthURLs here! Implement a separate function, or
	//       at least rename the old one.
	logger.Warnf(ctx, "FIXME: Do not use OAuthURLs for 'OpenBrowser'!")

	grpc.OAuthURLHandlerLocker.Lock()
	logger.Debugf(ctx, "grpc.OAuthURLHandlerLocker.Lock()-ed")
	defer logger.Debugf(ctx, "grpc.OAuthURLHandlerLocker.Unlock()-ed")
	defer grpc.OAuthURLHandlerLocker.Unlock()

	req := streamd_grpc.OAuthRequest{
		PlatID:  string("<OpenBrowser>"),
		AuthURL: url,
	}

	count := 0
	for _, handlers := range grpc.OAuthURLHandlers {
		logger.Debugf(ctx, "OpenOAuthURL() sending %#+v", req)
		var resultErr *multierror.Error
		for _, handler := range handlers {
			count++
			err := handler.Sender.Send(&req)
			if err != nil {
				err = multierror.Append(resultErr, fmt.Errorf("unable to send oauth request: %w", err))
			}
		}
	}

	if count == 0 {
		return ErrNoOAuthHandlerForPort{}
	}
	return nil
}

func (grpc *GRPCServer) OpenOAuthURL(
	ctx context.Context,
	listenPort uint16,
	platID streamcontrol.PlatformName,
	authURL string,
) (_ret error) {
	logger.Debugf(ctx, "OpenOAuthURL(ctx, %d, '%s', '%s')", listenPort, platID, authURL)
	defer func() {
		logger.Debugf(ctx, "/OpenOAuthURL(ctx, %d, '%s', '%s'): %v", listenPort, platID, authURL, _ret)
	}()

	grpc.OAuthURLHandlerLocker.Lock()
	logger.Debugf(ctx, "grpc.OAuthURLHandlerLocker.Lock()-ed")
	defer logger.Debugf(ctx, "grpc.OAuthURLHandlerLocker.Unlock()-ed")
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
	logger.Debugf(ctx, "OpenOAuthURL() sending %#+v", req)
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

	var result []*streamd_grpc.StreamServerWithStatistics
	for _, srv := range servers {
		t, err := goconv.StreamServerTypeGo2GRPC(srv.Type)
		if err != nil {
			return nil, fmt.Errorf("unable to convert the server type value: %w", err)
		}

		result = append(result, &streamd_grpc.StreamServerWithStatistics{
			Config: &streamd_grpc.StreamServer{
				ServerType: t,
				ListenAddr: srv.ListenAddr,
			},
			Statistics: &streamd_grpc.StreamServerStatistics{
				NumBytesConsumerWrote: int64(srv.NumBytesConsumerWrote),
				NumBytesProducerRead:  int64(srv.NumBytesProducerRead),
			},
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
	t, err := goconv.StreamServerTypeGRPC2Go(req.GetConfig().GetServerType())
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
			DestinationID: string(dst.ID),
			Url:           dst.URL,
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
		api.DestinationID(req.GetConfig().GetDestinationID()),
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
		api.DestinationID(req.GetDestinationID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamDestinationReply{}, nil
}

func (grpc *GRPCServer) AddIncomingStream(
	ctx context.Context,
	req *streamd_grpc.AddIncomingStreamRequest,
) (*streamd_grpc.AddIncomingStreamReply, error) {
	err := grpc.StreamD.AddIncomingStream(ctx, api.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddIncomingStreamReply{}, nil
}

func (grpc *GRPCServer) RemoveIncomingStream(
	ctx context.Context,
	req *streamd_grpc.RemoveIncomingStreamRequest,
) (*streamd_grpc.RemoveIncomingStreamReply, error) {
	err := grpc.StreamD.RemoveIncomingStream(ctx, api.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveIncomingStreamReply{}, nil
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

	var result []*streamd_grpc.StreamForwardWithStatistics
	for _, s := range streamFwds {
		item := &streamd_grpc.StreamForwardWithStatistics{
			Config: &streamd_grpc.StreamForward{
				StreamID:      string(s.StreamID),
				DestinationID: string(s.DestinationID),
				Enabled:       s.Enabled,
			},
			Statistics: &streamd_grpc.StreamForwardStatistics{
				NumBytesWrote: int64(s.NumBytesWrote),
				NumBytesRead:  int64(s.NumBytesRead),
			},
		}
		item.Config.Quirks = &streamd_grpc.StreamForwardQuirks{
			RestartUntilYoutubeRecognizesStream: &streamd_grpc.RestartUntilYoutubeRecognizesStream{
				Enabled:        s.Quirks.RestartUntilYoutubeRecognizesStream.Enabled,
				StartTimeout:   s.Quirks.RestartUntilYoutubeRecognizesStream.StartTimeout.Seconds(),
				StopStartDelay: s.Quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay.Seconds(),
			},
			StartAfterYoutubeRecognizedStream: &streamd_grpc.StartAfterYoutubeRecognizedStream{
				Enabled: s.Quirks.StartAfterYoutubeRecognizedStream.Enabled,
			},
		}
		result = append(result, item)
	}
	return &streamd_grpc.ListStreamForwardsReply{
		StreamForwards: result,
	}, nil
}

func (grpc *GRPCServer) AddStreamForward(
	ctx context.Context,
	req *streamd_grpc.AddStreamForwardRequest,
) (*streamd_grpc.AddStreamForwardReply, error) {
	cfg := req.GetConfig()
	err := grpc.StreamD.AddStreamForward(
		ctx,
		api.StreamID(req.GetConfig().GetStreamID()),
		api.DestinationID(req.GetConfig().GetDestinationID()),
		cfg.Enabled,
		api.StreamForwardingQuirks{
			RestartUntilYoutubeRecognizesStream: api.RestartUntilYoutubeRecognizesStream{
				Enabled:        cfg.Quirks.RestartUntilYoutubeRecognizesStream.Enabled,
				StartTimeout:   sec2dur(cfg.Quirks.RestartUntilYoutubeRecognizesStream.StartTimeout),
				StopStartDelay: sec2dur(cfg.Quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay),
			},
			StartAfterYoutubeRecognizedStream: api.StartAfterYoutubeRecognizedStream{
				Enabled: cfg.Quirks.StartAfterYoutubeRecognizedStream.Enabled,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamForwardReply{}, nil
}

func (grpc *GRPCServer) UpdateStreamForward(
	ctx context.Context,
	req *streamd_grpc.UpdateStreamForwardRequest,
) (*streamd_grpc.UpdateStreamForwardReply, error) {
	cfg := req.GetConfig()
	err := grpc.StreamD.UpdateStreamForward(
		ctx,
		api.StreamID(req.GetConfig().GetStreamID()),
		api.DestinationID(req.GetConfig().GetDestinationID()),
		cfg.Enabled,
		api.StreamForwardingQuirks{
			RestartUntilYoutubeRecognizesStream: api.RestartUntilYoutubeRecognizesStream{
				Enabled:        cfg.Quirks.RestartUntilYoutubeRecognizesStream.Enabled,
				StartTimeout:   sec2dur(cfg.Quirks.RestartUntilYoutubeRecognizesStream.StartTimeout),
				StopStartDelay: sec2dur(cfg.Quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay),
			},
			StartAfterYoutubeRecognizedStream: api.StartAfterYoutubeRecognizedStream{
				Enabled: cfg.Quirks.StartAfterYoutubeRecognizedStream.Enabled,
			},
		},
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.UpdateStreamForwardReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamForward(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamForwardRequest,
) (*streamd_grpc.RemoveStreamForwardReply, error) {
	err := grpc.StreamD.RemoveStreamForward(
		ctx,
		api.StreamID(req.GetConfig().GetStreamID()),
		api.DestinationID(req.GetConfig().GetDestinationID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamForwardReply{}, nil
}

func (grpc *GRPCServer) WaitForStreamPublisher(
	req *streamd_grpc.WaitForStreamPublisherRequest,
	sender streamd_grpc.StreamD_WaitForStreamPublisherServer,
) (_ret error) {
	ctx := sender.Context()
	logger.Tracef(ctx, "WaitForStreamPublisher(): StreamID:%s", req.GetStreamID())
	defer func() {
		logger.Tracef(ctx, "/WaitForStreamPublisher(): StreamID:%s: %v", req.GetStreamID(), _ret)
	}()

	ch, err := grpc.StreamD.WaitForStreamPublisher(
		ctx,
		api.StreamID(req.GetStreamID()),
	)
	if err != nil {
		return fmt.Errorf("streamd returned error: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
	}

	return sender.Send(&streamd_grpc.StreamPublisher{})
}

func (grpc *GRPCServer) AddStreamPlayer(
	ctx context.Context,
	req *streamd_grpc.AddStreamPlayerRequest,
) (_ret *streamd_grpc.AddStreamPlayerReply, _err error) {
	cfg := req.GetConfig()
	logger.Tracef(ctx, "AddStreamPlayer(): StreamID:%s", cfg.StreamID)
	defer func() {
		logger.Tracef(ctx, "/AddStreamPlayer(): StreamID:%s: %v", cfg.StreamID, _err)
	}()

	err := grpc.StreamD.AddStreamPlayer(
		ctx,
		streamtypes.StreamID(cfg.GetStreamID()),
		goconv.StreamPlayerTypeGRPC2Go(cfg.PlayerType),
		cfg.GetDisabled(),
		goconv.StreamPlaybackConfigGRPC2Go(cfg.GetStreamPlaybackConfig()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamPlayerReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamPlayer(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamPlayerRequest,
) (_req *streamd_grpc.RemoveStreamPlayerReply, _err error) {
	logger.Tracef(ctx, "AddStreamPlayer(): StreamID:%s", req.GetStreamID())
	defer func() {
		logger.Tracef(ctx, "/AddStreamPlayer(): StreamID:%s: %v", req.GetStreamID(), _err)
	}()

	err := grpc.StreamD.RemoveStreamPlayer(
		ctx,
		streamtypes.StreamID(req.GetStreamID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamPlayerReply{}, nil
}

func (grpc *GRPCServer) UpdateStreamPlayer(
	ctx context.Context,
	req *streamd_grpc.UpdateStreamPlayerRequest,
) (_req *streamd_grpc.UpdateStreamPlayerReply, _err error) {
	cfg := req.GetConfig()
	logger.Tracef(ctx, "UpdateStreamPlayer(): StreamID:%s", cfg.StreamID)
	defer func() {
		logger.Tracef(ctx, "/UpdateStreamPlayer(): StreamID:%s: %v", cfg.StreamID, _err)
	}()

	err := grpc.StreamD.UpdateStreamPlayer(
		ctx,
		streamtypes.StreamID(cfg.GetStreamID()),
		goconv.StreamPlayerTypeGRPC2Go(cfg.PlayerType),
		cfg.GetDisabled(),
		goconv.StreamPlaybackConfigGRPC2Go(cfg.GetStreamPlaybackConfig()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.UpdateStreamPlayerReply{}, nil
}

func (grpc *GRPCServer) ListStreamPlayers(
	ctx context.Context,
	req *streamd_grpc.ListStreamPlayersRequest,
) (*streamd_grpc.ListStreamPlayersReply, error) {
	players, err := grpc.StreamD.ListStreamPlayers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*streamd_grpc.StreamPlayerConfig, 0, len(players))
	for _, player := range players {
		result = append(result, &streamd_grpc.StreamPlayerConfig{
			StreamID:             string(player.StreamID),
			PlayerType:           goconv.StreamPlayerTypeGo2GRPC(player.PlayerType),
			Disabled:             player.Disabled,
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(&player.StreamPlaybackConfig),
		})
	}
	return &streamd_grpc.ListStreamPlayersReply{
		Players: result,
	}, nil
}
func (grpc *GRPCServer) GetStreamPlayer(
	ctx context.Context,
	req *streamd_grpc.GetStreamPlayerRequest,
) (*streamd_grpc.GetStreamPlayerReply, error) {
	player, err := grpc.StreamD.GetStreamPlayer(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.GetStreamPlayerReply{
		Config: &streamd_grpc.StreamPlayerConfig{
			StreamID:             string(player.StreamID),
			PlayerType:           goconv.StreamPlayerTypeGo2GRPC(player.PlayerType),
			Disabled:             player.Disabled,
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(&player.StreamPlaybackConfig),
		},
	}, nil
}

func (grpc *GRPCServer) StreamPlayerOpen(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerOpenRequest,
) (*streamd_grpc.StreamPlayerOpenReply, error) {
	err := grpc.StreamD.StreamPlayerOpenURL(ctx, streamtypes.StreamID(req.GetStreamID()), req.GetRequest().GetLink())
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerOpenReply{
		Reply: &player_grpc.OpenReply{},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerProcessTitle(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerProcessTitleRequest,
) (*streamd_grpc.StreamPlayerProcessTitleReply, error) {
	title, err := grpc.StreamD.StreamPlayerProcessTitle(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerProcessTitleReply{
		Reply: &player_grpc.ProcessTitleReply{
			Title: title,
		},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerGetLink(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerGetLinkRequest,
) (*streamd_grpc.StreamPlayerGetLinkReply, error) {
	link, err := grpc.StreamD.StreamPlayerGetLink(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerGetLinkReply{
		Reply: &player_grpc.GetLinkReply{
			Link: link,
		},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerEndChan(
	req *streamd_grpc.StreamPlayerEndChanRequest,
	srv streamd_grpc.StreamD_StreamPlayerEndChanServer,
) error {
	ctx, cancelFn := context.WithCancel(srv.Context())
	defer cancelFn()
	ch, err := grpc.StreamD.StreamPlayerEndChan(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
	}
	return srv.Send(&streamd_grpc.StreamPlayerEndChanReply{
		Reply: &player_grpc.EndChanReply{},
	})
}
func (grpc *GRPCServer) StreamPlayerIsEnded(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerIsEndedRequest,
) (*streamd_grpc.StreamPlayerIsEndedReply, error) {
	isEnded, err := grpc.StreamD.StreamPlayerIsEnded(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerIsEndedReply{
		Reply: &player_grpc.IsEndedReply{
			IsEnded: isEnded,
		},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerGetPosition(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerGetPositionRequest,
) (*streamd_grpc.StreamPlayerGetPositionReply, error) {
	pos, err := grpc.StreamD.StreamPlayerGetPosition(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerGetPositionReply{
		Reply: &player_grpc.GetPositionReply{
			PositionSecs: pos.Seconds(),
		},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerGetLength(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerGetLengthRequest,
) (*streamd_grpc.StreamPlayerGetLengthReply, error) {
	l, err := grpc.StreamD.StreamPlayerGetPosition(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerGetLengthReply{
		Reply: &player_grpc.GetLengthReply{
			LengthSecs: l.Seconds(),
		},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerSetSpeed(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerSetSpeedRequest,
) (*streamd_grpc.StreamPlayerSetSpeedReply, error) {
	err := grpc.StreamD.StreamPlayerSetSpeed(ctx, streamtypes.StreamID(req.GetStreamID()), req.GetRequest().Speed)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerSetSpeedReply{
		Reply: &player_grpc.SetSpeedReply{},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerSetPause(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerSetPauseRequest,
) (*streamd_grpc.StreamPlayerSetPauseReply, error) {
	err := grpc.StreamD.StreamPlayerSetPause(ctx, streamtypes.StreamID(req.GetStreamID()), req.GetRequest().SetPaused)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerSetPauseReply{
		Reply: &player_grpc.SetPauseReply{},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerStop(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerStopRequest,
) (*streamd_grpc.StreamPlayerStopReply, error) {
	err := grpc.StreamD.StreamPlayerStop(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerStopReply{
		Reply: &player_grpc.StopReply{},
	}, nil
}
func (grpc *GRPCServer) StreamPlayerClose(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerCloseRequest,
) (*streamd_grpc.StreamPlayerCloseReply, error) {
	err := grpc.StreamD.StreamPlayerClose(ctx, streamtypes.StreamID(req.GetStreamID()))
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerCloseReply{
		Reply: &player_grpc.CloseReply{},
	}, nil
}

type sender[T any] interface {
	grpc.ServerStream

	Context() context.Context
	Send(*T) error
}

func wrapChan[T any, E any](
	getChan func(ctx context.Context) (<-chan E, error),
	sender sender[T],
) error {
	ctx, cancelFn := context.WithCancel(sender.Context())
	defer cancelFn()
	ch, err := getChan(ctx)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
		var result T
		err := sender.Send(&result)
		if err != nil {
			return fmt.Errorf("unable to send %#+v: %w", result, err)
		}
	}
}

func (grpc *GRPCServer) SubscribeToConfigChanges(
	req *streamd_grpc.SubscribeToConfigChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToConfigChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToConfigChanges,
		srv,
	)
}
func (grpc *GRPCServer) SubscribeToStreamsChanges(
	req *streamd_grpc.SubscribeToStreamsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamsChanges,
		srv,
	)
}
func (grpc *GRPCServer) SubscribeToStreamServersChanges(
	req *streamd_grpc.SubscribeToStreamServersChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamServersChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamServersChanges,
		srv,
	)
}
func (grpc *GRPCServer) SubscribeToStreamDestinationsChanges(
	req *streamd_grpc.SubscribeToStreamDestinationsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamDestinationsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamDestinationsChanges,
		srv,
	)
}
func (grpc *GRPCServer) SubscribeToIncomingStreamsChanges(
	req *streamd_grpc.SubscribeToIncomingStreamsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToIncomingStreamsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToIncomingStreamsChanges,
		srv,
	)
}
func (grpc *GRPCServer) SubscribeToStreamForwardsChanges(
	req *streamd_grpc.SubscribeToStreamForwardsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamForwardsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamForwardsChanges,
		srv,
	)
}

func (grpc *GRPCServer) SubscribeToStreamPlayersChanges(
	req *streamd_grpc.SubscribeToStreamPlayersChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamPlayersChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamPlayersChanges,
		srv,
	)
}
