package server

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/google/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/player/pkg/player/protobuf/go/player_grpc"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/memoize"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xgrpc"
	"github.com/xaionaro-go/xsync"
)

type oauthURLHandlers map[uint16]map[uuid.UUID]*OAuthURLHandler

type GRPCServer struct {
	streamd_grpc.UnimplementedStreamDServer
	StreamD               api.StreamD
	MemoizeDataValue      *memoize.MemoizeData
	OAuthURLHandlerLocker xsync.Mutex
	OAuthURLHandlers      oauthURLHandlers

	UnansweredOAuthRequestsLocker xsync.Mutex
	UnansweredOAuthRequests       map[streamcontrol.PlatformID]map[uint16]*streamd_grpc.OAuthRequest
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

		OAuthURLHandlers:        oauthURLHandlers{},
		UnansweredOAuthRequests: map[streamcontrol.PlatformID]map[uint16]*streamd_grpc.OAuthRequest{},
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
	totalSize := len(
		req.GetPayloadToReturn(),
	) + int(
		extraSize,
	)
	if totalSize > 65535 {
		return nil, fmt.Errorf(
			"requested a too big payload",
		)
	}
	payload.WriteString(req.GetPayloadToReturn())
	payload.WriteString(strings.Repeat("0", int(extraSize)))
	return &streamd_grpc.PingReply{
		Payload: payload.String(),
	}, nil
}

func (grpc *GRPCServer) Close() error {
	err := &multierror.Error{}
	ctx := context.TODO()
	hs := xsync.DoR1(
		ctx,
		&grpc.OAuthURLHandlerLocker,
		func() oauthURLHandlers {
			return grpc.OAuthURLHandlers
		},
	)
	for listenPort, sender := range hs {
		_ = sender // TODO: invent sender.Close()
		delete(grpc.OAuthURLHandlers, listenPort)
	}
	return err.ErrorOrNil()
}

func (grpc *GRPCServer) invalidateCache(
	ctx context.Context,
) {
	grpc.MemoizeDataValue.InvalidateCache(ctx)
}

func (grpc *GRPCServer) SetLoggingLevel(
	ctx context.Context,
	req *streamd_grpc.SetLoggingLevelRequest,
) (*streamd_grpc.SetLoggingLevelReply, error) {
	err := grpc.StreamD.SetLoggingLevel(
		ctx,
		goconv.LoggingLevelGRPC2Go(req.GetLoggingLevel()),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to set the logging level: %w",
			err,
		)
	}
	return &streamd_grpc.SetLoggingLevelReply{}, nil
}

func (grpc *GRPCServer) GetLoggingLevel(
	ctx context.Context,
	req *streamd_grpc.GetLoggingLevelRequest,
) (*streamd_grpc.GetLoggingLevelReply, error) {
	level, err := grpc.StreamD.GetLoggingLevel(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to query the logging level: %w",
			err,
		)
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
		return nil, fmt.Errorf(
			"unable to get the config: %w",
			err,
		)
	}
	var buf bytes.Buffer
	_, err = cfg.WriteTo(&buf)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to serialize the config: %w",
			err,
		)
	}
	return &streamd_grpc.GetConfigReply{
		Config: buf.String(),
	}, nil
}

func (grpc *GRPCServer) ListProfiles(
	ctx context.Context,
	req *streamd_grpc.ListProfilesRequest,
) (*streamd_grpc.ListProfilesReply, error) {
	cfg, err := grpc.StreamD.GetConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get config: %w", err)
	}

	var profiles []streamcontrol.ProfileName
	for profileName := range cfg.ProfileMetadata {
		profiles = append(profiles, profileName)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i] < profiles[j]
	})

	var resp streamd_grpc.ListProfilesReply
	for _, profileName := range profiles {
		resp.Profiles = append(resp.Profiles, &streamd_grpc.ProfileInfo{
			Name: string(profileName),
		})
	}
	return &resp, nil
}

func (grpc *GRPCServer) SetConfig(
	ctx context.Context,
	req *streamd_grpc.SetConfigRequest,
) (*streamd_grpc.SetConfigReply, error) {

	logger.Debugf(ctx, "received SetConfig: %s", req.Config)

	var result config.Config
	_, err := result.Read([]byte(req.Config))
	if err != nil {
		return nil, fmt.Errorf(
			"unable to unserialize the config: %w",
			err,
		)
	}

	err = grpc.StreamD.SetConfig(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to set the config: %w",
			err,
		)
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
		return nil, fmt.Errorf(
			"unable to save the config: %w",
			err,
		)
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
		return nil, fmt.Errorf(
			"unable to reset the cache: %w",
			err,
		)
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
		return nil, fmt.Errorf(
			"unable to init the cache: %w",
			err,
		)
	}
	grpc.invalidateCache(ctx)
	return &streamd_grpc.InitCacheReply{}, nil
}

func (grpc *GRPCServer) SetStreamActive(
	ctx context.Context,
	req *streamd_grpc.SetStreamActiveRequest,
) (*streamd_grpc.SetStreamActiveReply, error) {
	err := grpc.StreamD.SetStreamActive(
		ctx,
		goconv.StreamIDFullyQualifiedFromGRPC(req.GetId()),
		req.GetIsActive(),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to set stream active state: %w", err)
	}

	grpc.invalidateCache(ctx)
	return &streamd_grpc.SetStreamActiveReply{}, nil
}

func (grpc *GRPCServer) GetStreamD() api.StreamD {
	if grpc.StreamD == nil {
		panic("grpc.StreamD == nil")
	}
	streamD, ok := grpc.StreamD.(*streamd.StreamD)
	if !ok {
		return grpc.StreamD
	}

	<-streamD.ReadyChan
	return streamD
}

func (grpc *GRPCServer) IsBackendEnabled(
	ctx context.Context,
	req *streamd_grpc.IsBackendEnabledRequest,
) (_ret *streamd_grpc.IsBackendEnabledReply, _err error) {
	platID := streamcontrol.PlatformID(req.GetPlatID())
	logger.Tracef(ctx, "IsBackendEnabled(ctx, '%s')", platID)
	defer func() { logger.Tracef(ctx, "/IsBackendEnabled(ctx, '%s'): %v %v", platID, _ret, _err) }()
	enabled, err := grpc.GetStreamD().IsBackendEnabled(
		ctx,
		platID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to check if backend '%s' is enabled: %w",
			req.GetPlatID(),
			err,
		)
	}
	return &streamd_grpc.IsBackendEnabledReply{
		IsInitialized: enabled,
	}, nil
}

func (grpc *GRPCServer) GetBackendInfo(
	ctx context.Context,
	req *streamd_grpc.GetBackendInfoRequest,
) (_ret *streamd_grpc.GetBackendInfoReply, _err error) {
	platID, includeData := streamcontrol.PlatformID(req.GetPlatID()), req.GetIncludeData()
	logger.Tracef(ctx, "GetBackendInfo(ctx, '%s', %t)", platID, includeData)
	defer func() { logger.Tracef(ctx, "/GetBackendInfo(ctx, '%s', %t): %v %v", platID, includeData, _ret, _err) }()
	isEnabled, err := grpc.GetStreamD().IsBackendEnabled(
		ctx,
		platID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to check if the backend is enabled: %w",
			err,
		)
	}
	info, err := grpc.StreamD.GetBackendInfo(ctx, platID, includeData)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get the backend info: %w",
			err,
		)
	}

	result := &streamd_grpc.GetBackendInfoReply{
		IsInitialized: isEnabled,
		Capabilities:  goconv.CapabilitiesGo2GRPC(ctx, info.Capabilities),
	}

	if includeData {
		dataSerialized, err := json.Marshal(info.Data)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to serialize the backend info: %w",
				err,
			)
		}
		result.Data = string(dataSerialized)
	}

	return result, nil
}

func (grpc *GRPCServer) SetTitle(
	ctx context.Context,
	req *streamd_grpc.SetTitleRequest,
) (*streamd_grpc.SetTitleReply, error) {
	err := grpc.StreamD.SetTitle(
		ctx,
		goconv.StreamIDFullyQualifiedFromGRPC(req.GetId()),
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
		goconv.StreamIDFullyQualifiedFromGRPC(req.GetId()),
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
	id := goconv.StreamIDFullyQualifiedFromGRPC(req.GetId())
	profile, err := goconv.ProfileGRPC2Go(id.PlatformID, req.GetProfile())
	if err != nil {
		return nil, err
	}

	logger.Debugf(
		ctx,
		"unserialized profile: %#+v",
		profile,
	)

	err = grpc.StreamD.ApplyProfile(
		ctx,
		id,
		profile,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to apply the profile %#+v: %w",
			profile,
			err,
		)
	}
	return &streamd_grpc.ApplyProfileReply{}, nil
}

func (grpc *GRPCServer) EXPERIMENTAL_ReinitStreamControllers(
	ctx context.Context,
	in *streamd_grpc.EXPERIMENTAL_ReinitStreamControllersRequest,
) (*streamd_grpc.EXPERIMENTAL_ReinitStreamControllersReply, error) {
	err := grpc.StreamD.EXPERIMENTAL_ReinitStreamControllers(
		ctx,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"StreamD returned an error: %w",
			err,
		)
	}

	return &streamd_grpc.EXPERIMENTAL_ReinitStreamControllersReply{}, nil
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

	id := goconv.StreamIDFullyQualifiedFromGRPC(req.GetId())
	streamStatus, err := grpc.StreamD.GetStreamStatus(
		ctx,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get the stream status of '%s': %w",
			id,
			err,
		)
	}

	var startedAt *int64
	if streamStatus.StartedAt != nil {
		startedAt = ptr(streamStatus.StartedAt.UnixNano())
	}

	var viewersCount *uint64
	if streamStatus.ViewersCount != nil {
		viewersCount = ptr(uint64(*streamStatus.ViewersCount))
	}

	var customData string
	if streamStatus.CustomData != nil {
		customDataSerialized, err := json.Marshal(
			streamStatus.CustomData,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to serialize the custom data: %w",
				err,
			)
		}
		customData = string(customDataSerialized)
	}

	return &streamd_grpc.GetStreamStatusReply{
		IsActive:     streamStatus.IsActive,
		StartedAt:    startedAt,
		CustomData:   customData,
		ViewersCount: viewersCount,
	}, nil
}

func (grpc *GRPCServer) GetStreams(
	ctx context.Context,
	req *streamd_grpc.GetStreamsRequest,
) (*streamd_grpc.GetStreamsReply, error) {
	accIDsG := req.GetAccountIDs()
	accIDs := make([]streamcontrol.AccountIDFullyQualified, 0, len(accIDsG))
	for _, id := range accIDsG {
		accIDs = append(accIDs, goconv.AccountIDFullyQualifiedFromGRPC(id))
	}
	streams, err := grpc.StreamD.GetStreams(
		ctx,
		accIDs...,
	)
	if err != nil {
		return nil, err
	}
	result := make([]*streamd_grpc.StreamInfo, 0, len(streams))
	for _, stream := range streams {
		result = append(result, &streamd_grpc.StreamInfo{
			ID:   string(stream.ID),
			Name: stream.Name,
		})
	}
	return &streamd_grpc.GetStreamsReply{
		Streams: result,
	}, nil
}

func (grpc *GRPCServer) GetPlatforms(
	ctx context.Context,
	req *streamd_grpc.GetPlatformsRequest,
) (*streamd_grpc.GetPlatformsReply, error) {
	platIDs := grpc.StreamD.GetPlatforms(ctx)
	res := make([]string, 0, len(platIDs))
	for _, id := range platIDs {
		res = append(res, string(id))
	}
	return &streamd_grpc.GetPlatformsReply{
		PlatformIDs: res,
	}, nil
}

func (grpc *GRPCServer) GetAccounts(
	ctx context.Context,
	req *streamd_grpc.GetAccountsRequest,
) (*streamd_grpc.GetAccountsReply, error) {
	platIDsG := req.GetPlatformIDs()
	platIDs := make([]streamcontrol.PlatformID, 0, len(platIDsG))
	for _, id := range platIDsG {
		platIDs = append(platIDs, streamcontrol.PlatformID(id))
	}
	accounts, err := grpc.StreamD.GetAccounts(
		ctx,
		platIDs...,
	)
	if err != nil {
		return nil, err
	}
	result := make([]*streamd_grpc.AccountIDFullyQualified, 0, len(accounts))
	for _, id := range accounts {
		result = append(result, goconv.AccountIDFullyQualifiedToGRPC(id))
	}
	return &streamd_grpc.GetAccountsReply{
		AccountIDs: result,
	}, nil
}

func (grpc *GRPCServer) GetActiveStreamIDs(
	ctx context.Context,
	req *streamd_grpc.GetActiveStreamIDsRequest,
) (*streamd_grpc.GetActiveStreamIDsReply, error) {
	ids, err := grpc.StreamD.GetActiveStreamIDs(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*streamd_grpc.StreamIDFullyQualified, 0, len(ids))
	for _, id := range ids {
		result = append(result, goconv.StreamIDFullyQualifiedToGRPC(id))
	}
	return &streamd_grpc.GetActiveStreamIDsReply{
		StreamSourceIDs: result,
	}, nil
}

func (grpc *GRPCServer) SubscribeToOAuthRequests(
	req *streamd_grpc.SubscribeToOAuthRequestsRequest,
	sender streamd_grpc.StreamD_SubscribeToOAuthRequestsServer,
) (_ret error) {
	ctx, cancelFn := context.WithCancel(sender.Context())
	_uuid, err := uuid.NewRandom()
	if err != nil {
		logger.Errorf(
			ctx,
			"unable to generate an UUID: %v",
			err,
		)
	}

	logger.Tracef(
		ctx,
		"SubscribeToOAuthRequests(): UUID:%s",
		_uuid,
	)
	defer func() { logger.Tracef(ctx, "/SubscribeToOAuthRequests(): UUID:%s: %v", _uuid, _ret) }()

	listenPort := uint16(req.ListenPort)
	streamD, _ := grpc.StreamD.(*streamd.StreamD)

	grpc.OAuthURLHandlerLocker.Do(ctx, func() {
		m := grpc.OAuthURLHandlers[listenPort]
		if m == nil {
			m = map[uuid.UUID]*OAuthURLHandler{}
		}
		m[_uuid] = &OAuthURLHandler{
			Sender:   sender,
			CancelFn: cancelFn,
		}
		grpc.OAuthURLHandlers[listenPort] = m // unnecessary, but feels safer
	})

	var unansweredRequests []*streamd_grpc.OAuthRequest
	grpc.UnansweredOAuthRequestsLocker.Do(ctx, func() {
		for _, m := range grpc.UnansweredOAuthRequests {
			req := m[listenPort]
			if req == nil {
				continue
			}
			unansweredRequests = append(
				unansweredRequests,
				req,
			)
		}
	})

	for _, req := range unansweredRequests {
		logger.Tracef(ctx, "re-sending an unanswered request to a new client: %#+v", req)
		err := sender.Send(req)
		errmon.ObserveErrorCtx(ctx, err)
	}

	if streamD != nil {
		streamD.AddOAuthListenPort(listenPort)
	}

	logger.Tracef(
		ctx,
		"waiting for the subscription to be cancelled",
	)

	// waiting, while normal actual sending happens via grpc.OAuthURLHandlers[listenPort]
	<-ctx.Done()

	grpc.OAuthURLHandlerLocker.Do(ctx, func() {
		delete(grpc.OAuthURLHandlers[listenPort], _uuid)
		if len(grpc.OAuthURLHandlers[listenPort]) == 0 {
			delete(grpc.OAuthURLHandlers, listenPort)
			if streamD != nil {
				streamD.RemoveOAuthListenPort(listenPort)
			}
		}
	})

	return nil
}

type ErrNoOAuthHandler struct {
	Err error
}

func (err ErrNoOAuthHandler) Error() string {
	return fmt.Sprintf("no oauth handler: %v", err.Err)
}

func (err ErrNoOAuthHandler) Unwrap() error {
	return err.Err
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
) (_err error) {
	logger.Debugf(ctx, "OpenBrowser(ctx, '%s')", url)
	defer func() { logger.Debugf(ctx, "/OpenBrowser(ctx, '%s'): %v", url, _err) }()

	// TODO: Stop abusing the function for OAuthURLs here! Implement a separate function, or
	//       at least rename the old one.
	logger.Warnf(
		ctx,
		"FIXME: Do not use OAuthURLs for 'OpenBrowser'!",
	)

	return xsync.DoA2R1(
		ctx,
		&grpc.OAuthURLHandlerLocker,
		grpc.openBrowser,
		ctx,
		url,
	)
}

func (grpc *GRPCServer) openBrowser(
	ctx context.Context,
	url string,
) (_ret error) {
	req := &streamd_grpc.OAuthRequest{
		PlatID:  string("<OpenBrowser>"),
		AuthURL: url,
	}

	var resultErr *multierror.Error
	successCount := 0
	for _, handlers := range grpc.OAuthURLHandlers {
		logger.Debugf(ctx, "openBrowser: OpenOAuthURL() sending %#+v", req)
		for _, handler := range handlers {
			err := handler.Sender.Send(req)
			if err != nil {
				resultErr = multierror.Append(resultErr, fmt.Errorf("unable to send oauth request: %w", err))
				continue
			}
			successCount++
		}
	}

	if successCount == 0 {
		err := resultErr.ErrorOrNil()
		if err == nil {
			err = fmt.Errorf("no handlers available")
		}
		return ErrNoOAuthHandler{Err: err}
	}

	return nil
}

func (grpc *GRPCServer) OpenOAuthURL(
	ctx context.Context,
	listenPort uint16,
	platID streamcontrol.PlatformID,
	authURL string,
) (_ret error) {
	logger.Debugf(ctx, "OpenOAuthURL(ctx, %d, '%s', '%s')", listenPort, platID, authURL)
	defer func() {
		logger.Debugf(ctx, "/OpenOAuthURL(ctx, %d, '%s', '%s'): %v", listenPort, platID, authURL, _ret)
	}()

	return xsync.DoA4R1(
		ctx,
		&grpc.OAuthURLHandlerLocker,
		grpc.openOAuthURL,
		ctx,
		listenPort,
		platID,
		authURL,
	)
}

func (grpc *GRPCServer) openOAuthURL(
	ctx context.Context,
	listenPort uint16,
	platID streamcontrol.PlatformID,
	authURL string,
) (_ret error) {
	handlers := grpc.OAuthURLHandlers[listenPort]
	if len(handlers) == 0 {
		return ErrNoOAuthHandlerForPort{
			Port: listenPort,
		}
	}
	req := &streamd_grpc.OAuthRequest{
		PlatID:  string(platID),
		AuthURL: authURL,
	}
	grpc.UnansweredOAuthRequestsLocker.Do(ctx, func() {
		if grpc.UnansweredOAuthRequests[platID] == nil {
			grpc.UnansweredOAuthRequests[platID] = map[uint16]*streamd_grpc.OAuthRequest{}
		}
		grpc.UnansweredOAuthRequests[platID][listenPort] = req
	})
	logger.Debugf(ctx, "openOAuthURL: OpenOAuthURL() sending %#+v", req)
	var resultErr *multierror.Error
	for _, handler := range handlers {
		err := handler.Sender.Send(req)
		if err != nil {
			resultErr = multierror.Append(resultErr, fmt.Errorf("unable to send oauth request: %w", err))
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
		return nil, fmt.Errorf(
			"unable to get variable '%s': %w",
			key,
			err,
		)
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
		return nil, fmt.Errorf(
			"unexpected hash type: %v",
			hashTypeIn,
		)
	}

	key := consts.VarKey(req.GetKey())
	b, err := grpc.StreamD.GetVariableHash(
		ctx,
		key,
		hashType,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get variable '%s': %w",
			key,
			err,
		)
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
	err := grpc.StreamD.SetVariable(
		ctx,
		key,
		req.GetValue(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to set variable '%s': %w",
			key,
			err,
		)
	}

	return &streamd_grpc.SetVariableReply{}, nil
}

func (grpc *GRPCServer) SubscribeToVariable(
	req *streamd_grpc.SubscribeToVariableRequest,
	srv streamd_grpc.StreamD_SubscribeToVariableServer,
) error {
	return wrapChan(
		func(ctx context.Context) (<-chan api.VariableValue, error) {
			return grpc.StreamD.SubscribeToVariable(ctx, consts.VarKey(req.GetKey()))
		},
		srv,
		func(input api.VariableValue) streamd_grpc.VariableChange {
			return streamd_grpc.VariableChange{
				Value: input,
			}
		},
	)
}

func (grpc *GRPCServer) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	grpc.UnansweredOAuthRequestsLocker.Do(ctx, func() {
		delete(
			grpc.UnansweredOAuthRequests,
			streamcontrol.PlatformID(req.PlatID),
		)
	})

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
		srvGRPC, err := goconv.StreamServerConfigGo2GRPC(
			ctx,
			srv.Type,
			srv.ListenAddr,
			srv.Options(),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to convert the server server value: %w",
				err,
			)
		}

		result = append(
			result,
			&streamd_grpc.StreamServerWithStatistics{
				Config: srvGRPC,
				Statistics: &streamd_grpc.StreamServerStatistics{
					NumBytesConsumerWrote: int64(
						srv.NumBytesConsumerWrote,
					),
					NumBytesProducerRead: int64(
						srv.NumBytesProducerRead,
					),
				},
			},
		)
	}
	return &streamd_grpc.ListStreamServersReply{
		StreamServers: result,
	}, nil
}

func (grpc *GRPCServer) StartStreamServer(
	ctx context.Context,
	req *streamd_grpc.StartStreamServerRequest,
) (*streamd_grpc.StartStreamServerReply, error) {
	srvType, addr, opts, err := goconv.StreamServerConfigGRPC2Go(
		ctx,
		req.GetConfig(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to convert the stream server config %#+v: %w",
			req.GetConfig(),
			err,
		)
	}

	err = grpc.StreamD.StartStreamServer(
		ctx,
		srvType,
		addr,
		opts...,
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

func (grpc *GRPCServer) ListStreamSinks(
	ctx context.Context,
	req *streamd_grpc.ListStreamSinksRequest,
) (*streamd_grpc.ListStreamSinksReply, error) {
	sinks, err := grpc.StreamD.ListStreamSinks(
		ctx,
	)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.StreamSink
	for _, sink := range sinks {
		result = append(
			result,
			&streamd_grpc.StreamSink{
				StreamSinkID: goconv.StreamSinkIDFullyQualifiedToGRPC(sink.ID),
				Config: &streamd_grpc.StreamSinkConfig{
					Url:            sink.URL,
					StreamKey:      sink.StreamKey.Get(),
					StreamSourceID: goconv.StreamIDFullyQualifiedToGRPC(sink.StreamSourceID.Deref()),
				},
			},
		)
	}
	return &streamd_grpc.ListStreamSinksReply{
		StreamSinks: result,
	}, nil
}

func (grpc *GRPCServer) AddStreamSink(
	ctx context.Context,
	req *streamd_grpc.AddStreamSinkRequest,
) (*streamd_grpc.AddStreamSinkReply, error) {
	var streamSourceID *streamcontrol.StreamIDFullyQualified
	if req.GetConfig().GetConfig().GetStreamSourceID() != nil {
		id := goconv.StreamIDFullyQualifiedFromGRPC(req.GetConfig().GetConfig().GetStreamSourceID())
		streamSourceID = &id
	}
	err := grpc.StreamD.AddStreamSink(
		ctx,
		goconv.StreamSinkIDFullyQualifiedFromGRPC(req.GetConfig().GetStreamSinkID()),
		types.StreamSinkConfig{
			URL:            req.GetConfig().GetConfig().GetUrl(),
			StreamKey:      secret.New[string](req.GetConfig().GetConfig().GetStreamKey()),
			StreamSourceID: streamSourceID,
		},
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamSinkReply{}, nil
}

func (grpc *GRPCServer) UpdateStreamSink(
	ctx context.Context,
	req *streamd_grpc.UpdateStreamSinkRequest,
) (*streamd_grpc.UpdateStreamSinkReply, error) {
	var streamSourceID *streamcontrol.StreamIDFullyQualified
	if req.GetConfig().GetConfig().GetStreamSourceID() != nil {
		id := goconv.StreamIDFullyQualifiedFromGRPC(req.GetConfig().GetConfig().GetStreamSourceID())
		streamSourceID = &id
	}
	err := grpc.StreamD.UpdateStreamSink(
		ctx,
		goconv.StreamSinkIDFullyQualifiedFromGRPC(req.GetConfig().GetStreamSinkID()),
		types.StreamSinkConfig{
			URL:            req.GetConfig().GetConfig().GetUrl(),
			StreamKey:      secret.New[string](req.GetConfig().GetConfig().GetStreamKey()),
			StreamSourceID: streamSourceID,
		},
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.UpdateStreamSinkReply{}, nil
}

func (grpc *GRPCServer) WaitStreamStarted(
	ctx context.Context,
	req *streamd_grpc.WaitStreamStartedRequest,
) (*streamd_grpc.WaitStreamStartedReply, error) {
	err := grpc.StreamD.WaitStreamStartedByStreamSourceID(
		ctx,
		goconv.StreamIDFullyQualifiedFromGRPC(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.WaitStreamStartedReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamSink(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamSinkRequest,
) (*streamd_grpc.RemoveStreamSinkReply, error) {
	err := grpc.StreamD.RemoveStreamSink(
		ctx,
		goconv.StreamSinkIDFullyQualifiedFromGRPC(req.GetStreamSinkID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamSinkReply{}, nil
}

func (grpc *GRPCServer) AddStreamSource(
	ctx context.Context,
	req *streamd_grpc.AddStreamSourceRequest,
) (*streamd_grpc.AddStreamSourceReply, error) {
	err := grpc.StreamD.AddStreamSource(
		ctx,
		api.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.AddStreamSourceReply{}, nil
}

func (grpc *GRPCServer) RemoveStreamSource(
	ctx context.Context,
	req *streamd_grpc.RemoveStreamSourceRequest,
) (*streamd_grpc.RemoveStreamSourceReply, error) {
	err := grpc.StreamD.RemoveStreamSource(
		ctx,
		api.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveStreamSourceReply{}, nil
}

func (grpc *GRPCServer) ListStreamSources(
	ctx context.Context,
	req *streamd_grpc.ListStreamSourcesRequest,
) (*streamd_grpc.ListStreamSourcesReply, error) {
	inStreams, err := grpc.StreamD.ListStreamSources(
		ctx,
	)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.StreamSource
	for _, s := range inStreams {
		result = append(
			result,
			&streamd_grpc.StreamSource{
				StreamSourceID: string(s.StreamSourceID),
				IsActive:       s.IsActive,
			},
		)
	}
	return &streamd_grpc.ListStreamSourcesReply{
		StreamSources: result,
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
				StreamSourceID: string(s.StreamSourceID),
				StreamSinkID:   goconv.StreamSinkIDFullyQualifiedToGRPC(s.StreamSinkID),
				Enabled:        s.Enabled,
			},
			Statistics: &streamd_grpc.StreamForwardStatistics{
				NumBytesWrote: int64(s.NumBytesWrote),
				NumBytesRead:  int64(s.NumBytesRead),
			},
		}
		item.Config.Encode = goconv.EncoderConfigToThrift(
			s.Encode.Enabled,
			s.Encode.EncodersConfig,
		)
		item.Config.Quirks = &streamd_grpc.StreamForwardQuirks{
			RestartUntilPlatformRecognizesStream: &streamd_grpc.RestartUntilPlatformRecognizesStream{
				Enabled:        s.Quirks.RestartUntilPlatformRecognizesStream.Enabled,
				StartTimeout:   s.Quirks.RestartUntilPlatformRecognizesStream.StartTimeout.Seconds(),
				StopStartDelay: s.Quirks.RestartUntilPlatformRecognizesStream.StopStartDelay.Seconds(),
			},
			WaitUntilPlatformRecognizesStream: &streamd_grpc.WaitUntilPlatformRecognizesStream{
				Enabled: s.Quirks.WaitUntilPlatformRecognizesStream.Enabled,
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
	encode, recodingEnabled := goconv.EncoderConfigFromThrift(cfg.Encode)
	err := grpc.StreamD.AddStreamForward(
		ctx,
		api.StreamSourceID(req.GetConfig().GetStreamSourceID()),
		goconv.StreamSinkIDFullyQualifiedFromGRPC(
			req.GetConfig().GetStreamSinkID(),
		),
		cfg.Enabled,
		types.EncodeConfig{
			Enabled:        recodingEnabled,
			EncodersConfig: encode,
		},
		api.StreamForwardingQuirks{
			RestartUntilPlatformRecognizesStream: api.RestartUntilPlatformRecognizesStream{
				Enabled: cfg.Quirks.RestartUntilPlatformRecognizesStream.Enabled,
				StartTimeout: sec2dur(
					cfg.Quirks.RestartUntilPlatformRecognizesStream.StartTimeout,
				),
				StopStartDelay: sec2dur(
					cfg.Quirks.RestartUntilPlatformRecognizesStream.StopStartDelay,
				),
			},
			WaitUntilPlatformRecognizesStream: api.WaitUntilPlatformRecognizesStream{
				Enabled: cfg.Quirks.WaitUntilPlatformRecognizesStream.Enabled,
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
	encode, recodingEnabled := goconv.EncoderConfigFromThrift(cfg.Encode)
	err := grpc.StreamD.UpdateStreamForward(
		ctx,
		api.StreamSourceID(req.GetConfig().GetStreamSourceID()),
		goconv.StreamSinkIDFullyQualifiedFromGRPC(
			req.GetConfig().GetStreamSinkID(),
		),
		cfg.Enabled,
		types.EncodeConfig{
			Enabled:        recodingEnabled,
			EncodersConfig: encode,
		},
		api.StreamForwardingQuirks{
			RestartUntilPlatformRecognizesStream: api.RestartUntilPlatformRecognizesStream{
				Enabled: cfg.Quirks.RestartUntilPlatformRecognizesStream.Enabled,
				StartTimeout: sec2dur(
					cfg.Quirks.RestartUntilPlatformRecognizesStream.StartTimeout,
				),
				StopStartDelay: sec2dur(
					cfg.Quirks.RestartUntilPlatformRecognizesStream.StopStartDelay,
				),
			},
			WaitUntilPlatformRecognizesStream: api.WaitUntilPlatformRecognizesStream{
				Enabled: cfg.Quirks.WaitUntilPlatformRecognizesStream.Enabled,
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
		api.StreamSourceID(req.GetConfig().GetStreamSourceID()),
		goconv.StreamSinkIDFullyQualifiedFromGRPC(
			req.GetConfig().GetStreamSinkID(),
		),
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
	logger.Tracef(
		ctx,
		"WaitForStreamPublisher(): StreamSourceID:%s",
		req.GetStreamSourceID(),
	)
	defer func() {
		logger.Tracef(
			ctx,
			"/WaitForStreamPublisher(): StreamSourceID:%s: %v",
			req.GetStreamSourceID(),
			_ret,
		)
	}()

	ch, err := grpc.StreamD.WaitForStreamPublisher(
		ctx,
		api.StreamSourceID(req.GetStreamSourceID()),
		req.GetWaitForNext(),
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
	logger.Tracef(
		ctx,
		"AddStreamPlayer(): StreamSourceID:%s",
		cfg.StreamSourceID,
	)
	defer func() {
		logger.Tracef(
			ctx,
			"/AddStreamPlayer(): StreamSourceID:%s: %v",
			cfg.StreamSourceID,
			_err,
		)
	}()

	err := grpc.StreamD.AddStreamPlayer(
		ctx,
		streamtypes.StreamSourceID(cfg.GetStreamSourceID()),
		goconv.StreamPlayerTypeGRPC2Go(cfg.PlayerType),
		cfg.GetDisabled(),
		goconv.StreamPlaybackConfigGRPC2Go(
			cfg.GetStreamPlaybackConfig(),
		),
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
	logger.Tracef(
		ctx,
		"AddStreamPlayer(): StreamSourceID:%s",
		req.GetStreamSourceID(),
	)
	defer func() {
		logger.Tracef(
			ctx,
			"/AddStreamPlayer(): StreamSourceID:%s: %v",
			req.GetStreamSourceID(),
			_err,
		)
	}()

	err := grpc.StreamD.RemoveStreamPlayer(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
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
	logger.Debugf(
		ctx,
		"UpdateStreamPlayer(): StreamSourceID:%s",
		cfg.StreamSourceID,
	)
	defer func() {
		logger.Debugf(
			ctx,
			"/UpdateStreamPlayer(): StreamSourceID:%s: %v",
			cfg.StreamSourceID,
			_err,
		)
	}()

	err := grpc.StreamD.UpdateStreamPlayer(
		ctx,
		streamtypes.StreamSourceID(cfg.GetStreamSourceID()),
		goconv.StreamPlayerTypeGRPC2Go(cfg.PlayerType),
		cfg.GetDisabled(),
		goconv.StreamPlaybackConfigGRPC2Go(
			cfg.GetStreamPlaybackConfig(),
		),
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
	result := make(
		[]*streamd_grpc.StreamPlayerConfig,
		0,
		len(players),
	)
	for _, player := range players {
		result = append(
			result,
			&streamd_grpc.StreamPlayerConfig{
				StreamSourceID: string(player.StreamSourceID),
				PlayerType: goconv.StreamPlayerTypeGo2GRPC(
					player.PlayerType,
				),
				Disabled: player.Disabled,
				StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(
					&player.StreamPlaybackConfig,
				),
			},
		)
	}
	return &streamd_grpc.ListStreamPlayersReply{
		Players: result,
	}, nil
}
func (grpc *GRPCServer) GetStreamPlayer(
	ctx context.Context,
	req *streamd_grpc.GetStreamPlayerRequest,
) (*streamd_grpc.GetStreamPlayerReply, error) {
	player, err := grpc.StreamD.GetStreamPlayer(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.GetStreamPlayerReply{
		Config: &streamd_grpc.StreamPlayerConfig{
			StreamSourceID: string(player.StreamSourceID),
			PlayerType: goconv.StreamPlayerTypeGo2GRPC(
				player.PlayerType,
			),
			Disabled: player.Disabled,
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(
				&player.StreamPlaybackConfig,
			),
		},
	}, nil
}

func (grpc *GRPCServer) StreamPlayerOpen(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerOpenRequest,
) (*streamd_grpc.StreamPlayerOpenReply, error) {
	err := grpc.StreamD.StreamPlayerOpenURL(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
		req.GetRequest().GetLink(),
	)
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
	title, err := grpc.StreamD.StreamPlayerProcessTitle(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	link, err := grpc.StreamD.StreamPlayerGetLink(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	ch, err := grpc.StreamD.StreamPlayerEndChan(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	isEnded, err := grpc.StreamD.StreamPlayerIsEnded(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	pos, err := grpc.StreamD.StreamPlayerGetPosition(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	l, err := grpc.StreamD.StreamPlayerGetPosition(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerGetLengthReply{
		Reply: &player_grpc.GetLengthReply{
			LengthSecs: l.Seconds(),
		},
	}, nil
}

func (grpc *GRPCServer) StreamPlayerGetLag(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerGetLagRequest,
) (*streamd_grpc.StreamPlayerGetLagReply, error) {
	l, replyTime, err := grpc.StreamD.StreamPlayerGetLag(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerGetLagReply{
		RequestUnixNano: req.GetRequestUnixNano(),
		ReplyUnixNano:   replyTime.UnixNano(),
		LagU:            l.Nanoseconds(),
	}, nil
}

func (grpc *GRPCServer) StreamPlayerSetSpeed(
	ctx context.Context,
	req *streamd_grpc.StreamPlayerSetSpeedRequest,
) (*streamd_grpc.StreamPlayerSetSpeedReply, error) {
	err := grpc.StreamD.StreamPlayerSetSpeed(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
		req.GetRequest().Speed,
	)
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
	err := grpc.StreamD.StreamPlayerSetPause(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
		req.GetRequest().IsPaused,
	)
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
	err := grpc.StreamD.StreamPlayerStop(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
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
	err := grpc.StreamD.StreamPlayerClose(
		ctx,
		streamtypes.StreamSourceID(req.GetStreamSourceID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.StreamPlayerCloseReply{
		Reply: &player_grpc.CloseReply{},
	}, nil
}

func wrapChan[T any, E any](
	getChan func(ctx context.Context) (<-chan E, error),
	sender xgrpc.Sender[T],
	parse func(E) T,
) (_err error) {
	ctx := sender.Context()
	return xgrpc.WrapChan(ctx, getChan, sender, func(input E) *T {
		return ptr(parse(input))
	})
}

func (grpc *GRPCServer) SubscribeToConfigChanges(
	req *streamd_grpc.SubscribeToConfigChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToConfigChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToConfigChanges,
		srv,
		func(input api.DiffConfig) streamd_grpc.ConfigChange {
			return streamd_grpc.ConfigChange{}
		},
	)
}
func (grpc *GRPCServer) SubscribeToStreamsChanges(
	req *streamd_grpc.SubscribeToStreamsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamsChanges,
		srv,
		func(input api.DiffStreams) streamd_grpc.StreamsChange {
			return streamd_grpc.StreamsChange{}
		},
	)
}
func (grpc *GRPCServer) SubscribeToStreamServersChanges(
	req *streamd_grpc.SubscribeToStreamServersChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamServersChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamServersChanges,
		srv,
		func(input api.DiffStreamServers) streamd_grpc.StreamServersChange {
			return streamd_grpc.StreamServersChange{}
		},
	)
}

func (grpc *GRPCServer) SubscribeToStreamSinksChanges(
	req *streamd_grpc.SubscribeToStreamSinksChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamSinksChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamSinksChanges,
		srv,
		func(input api.DiffStreamSinks) streamd_grpc.StreamSinksChange {
			return streamd_grpc.StreamSinksChange{}
		},
	)
}
func (grpc *GRPCServer) SubscribeToStreamSourcesChanges(
	req *streamd_grpc.SubscribeToStreamSourcesChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamSourcesChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamSourcesChanges,
		srv,
		func(input api.DiffStreamSources) streamd_grpc.StreamSourcesChange {
			return streamd_grpc.StreamSourcesChange{}
		},
	)
}
func (grpc *GRPCServer) SubscribeToStreamForwardsChanges(
	req *streamd_grpc.SubscribeToStreamForwardsChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamForwardsChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamForwardsChanges,
		srv,
		func(input api.DiffStreamForwards) streamd_grpc.StreamForwardsChange {
			return streamd_grpc.StreamForwardsChange{}
		},
	)
}

func (grpc *GRPCServer) SubscribeToStreamPlayersChanges(
	req *streamd_grpc.SubscribeToStreamPlayersChangesRequest,
	srv streamd_grpc.StreamD_SubscribeToStreamPlayersChangesServer,
) error {
	return wrapChan(
		grpc.StreamD.SubscribeToStreamPlayersChanges,
		srv,
		func(input api.DiffStreamPlayers) streamd_grpc.StreamPlayersChange {
			return streamd_grpc.StreamPlayersChange{}
		},
	)
}

func (grpc *GRPCServer) AddTimer(
	ctx context.Context,
	req *streamd_grpc.AddTimerRequest,
) (*streamd_grpc.AddTimerReply, error) {
	triggerAtUnixNano := req.GetTriggerAtUnixNano()
	triggerAt := time.Unix(
		triggerAtUnixNano/1000000000,
		triggerAtUnixNano%1000000000,
	)

	action, err := goconv.ActionGRPC2Go(req.Action)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to convert the action: %w",
			err,
		)
	}

	timerID, err := grpc.StreamD.AddTimer(
		ctx,
		triggerAt,
		action,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to add timer: %w",
			err,
		)
	}

	return &streamd_grpc.AddTimerReply{
		TimerID: int64(timerID),
	}, nil
}
func (grpc *GRPCServer) RemoveTimer(
	ctx context.Context,
	req *streamd_grpc.RemoveTimerRequest,
) (*streamd_grpc.RemoveTimerReply, error) {
	err := grpc.StreamD.RemoveTimer(
		ctx,
		api.TimerID(req.GetTimerID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveTimerReply{}, nil
}
func (grpc *GRPCServer) ListTimers(
	ctx context.Context,
	req *streamd_grpc.ListTimersRequest,
) (*streamd_grpc.ListTimersReply, error) {
	timers, err := grpc.StreamD.ListTimers(ctx)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.Timer
	for _, timer := range timers {
		resultAction, err := goconv.ActionGo2GRPC(
			timer.Action,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to convert the action: %w",
				err,
			)
		}
		result = append(result, &streamd_grpc.Timer{
			TimerID:           int64(timer.ID),
			TriggerAtUnixNano: timer.TriggerAt.UnixNano(),
			Action:            resultAction,
		})
	}
	return &streamd_grpc.ListTimersReply{
		Timers: result,
	}, nil
}

func (grpc *GRPCServer) ListTriggerRules(
	ctx context.Context,
	req *streamd_grpc.ListTriggerRulesRequest,
) (*streamd_grpc.ListTriggerRulesReply, error) {
	rules, err := grpc.StreamD.ListTriggerRules(ctx)
	if err != nil {
		return nil, err
	}

	var result []*streamd_grpc.TriggerRule
	for _, rule := range rules {
		triggerRule, err := goconv.TriggerRuleGo2GRPC(rule)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to convert the trigger rule: %w",
				err,
			)
		}
		result = append(
			result,
			triggerRule,
		)
	}
	return &streamd_grpc.ListTriggerRulesReply{
		Rules: result,
	}, nil
}

func (grpc *GRPCServer) AddTriggerRule(
	ctx context.Context,
	req *streamd_grpc.AddTriggerRuleRequest,
) (*streamd_grpc.AddTriggerRuleReply, error) {

	triggerRule, err := goconv.TriggerRuleGRPC2Go(req.GetRule())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the trigger rule: %w", err)
	}

	ruleID, err := grpc.StreamD.AddTriggerRule(ctx, triggerRule)
	if err != nil {
		return nil, fmt.Errorf("unable to add timer: %w", err)
	}

	return &streamd_grpc.AddTriggerRuleReply{
		RuleID: uint64(ruleID),
	}, nil
}

func (grpc *GRPCServer) RemoveTriggerRule(
	ctx context.Context,
	req *streamd_grpc.RemoveTriggerRuleRequest,
) (*streamd_grpc.RemoveTriggerRuleReply, error) {
	err := grpc.StreamD.RemoveTriggerRule(
		ctx,
		api.TriggerRuleID(req.GetRuleID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveTriggerRuleReply{}, nil
}

func (grpc *GRPCServer) UpdateTriggerRule(
	ctx context.Context,
	req *streamd_grpc.UpdateTriggerRuleRequest,
) (*streamd_grpc.UpdateTriggerRuleReply, error) {
	triggerRule, err := goconv.TriggerRuleGRPC2Go(req.GetRule())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the trigger rule: %w", err)
	}

	err = grpc.StreamD.UpdateTriggerRule(
		ctx,
		api.TriggerRuleID(req.GetRuleID()),
		triggerRule,
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.UpdateTriggerRuleReply{}, nil
}

func (grpc *GRPCServer) SubmitEvent(
	ctx context.Context,
	req *streamd_grpc.SubmitEventRequest,
) (*streamd_grpc.SubmitEventReply, error) {
	event, err := goconv.EventGRPC2Go(req.GetEvent())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the trigger rule: %w", err)
	}

	err = grpc.StreamD.SubmitEvent(ctx, event)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.SubmitEventReply{}, nil
}

func (grpc *GRPCServer) SubscribeToChatMessages(
	req *streamd_grpc.SubscribeToChatMessagesRequest,
	srv streamd_grpc.StreamD_SubscribeToChatMessagesServer,
) error {
	ts := req.GetSinceUNIXNano()
	limit := req.GetLimit()
	since := time.Unix(
		int64(ts)/int64(time.Second.Nanoseconds()),
		int64(ts)%int64(time.Second.Nanoseconds()),
	)
	return wrapChan(
		func(ctx context.Context) (<-chan api.ChatMessage, error) {
			return grpc.StreamD.SubscribeToChatMessages(ctx, since, limit)
		},
		srv,
		goconv.ChatMessageGo2GRPC,
	)
}

func (grpc *GRPCServer) SendChatMessage(
	ctx context.Context,
	req *streamd_grpc.SendChatMessageRequest,
) (*streamd_grpc.SendChatMessageReply, error) {
	err := grpc.StreamD.SendChatMessage(
		ctx,
		streamcontrol.PlatformID(req.PlatID),
		req.GetMessage(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.SendChatMessageReply{}, nil
}

func (grpc *GRPCServer) RemoveChatMessage(
	ctx context.Context,
	req *streamd_grpc.RemoveChatMessageRequest,
) (*streamd_grpc.RemoveChatMessageReply, error) {
	err := grpc.StreamD.RemoveChatMessage(
		ctx,
		streamcontrol.PlatformID(req.GetPlatID()),
		streamcontrol.EventID(req.GetMessageID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RemoveChatMessageReply{}, nil
}

func (grpc *GRPCServer) BanUser(
	ctx context.Context,
	req *streamd_grpc.BanUserRequest,
) (*streamd_grpc.BanUserReply, error) {

	var deadline time.Time
	if req.DeadlineUnixNano != nil {
		deadline = goconv.UnixGRPC2Go(*req.DeadlineUnixNano)
	}
	err := grpc.StreamD.BanUser(
		ctx,
		streamcontrol.PlatformID(req.GetPlatID()),
		streamcontrol.UserID(req.GetUserID()),
		req.GetReason(),
		deadline,
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.BanUserReply{}, nil
}

func (grpc *GRPCServer) Shoutout(
	ctx context.Context,
	req *streamd_grpc.ShoutoutRequest,
) (*streamd_grpc.ShoutoutReply, error) {
	err := grpc.StreamD.Shoutout(
		ctx,
		streamcontrol.PlatformID(req.GetPlatID()),
		streamcontrol.UserID(req.GetUserID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.ShoutoutReply{}, nil
}

func (grpc *GRPCServer) RaidTo(
	ctx context.Context,
	req *streamd_grpc.RaidToRequest,
) (*streamd_grpc.RaidToReply, error) {
	err := grpc.StreamD.RaidTo(
		ctx,
		streamcontrol.PlatformID(req.GetPlatID()),
		streamcontrol.UserID(req.GetUserID()),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.RaidToReply{}, nil
}

/*func (grpc *GRPCServer) ProxyConnect(
	req *streamd_grpc.ProxyConnectRequest,
	srv streamd_grpc.StreamD_ProxyConnectServer,
) error {
	grpc.StreamD.ProxyConnect(
		ctx,
	)
}

func (grpc *GRPCServer) ProxyPackets(
	streamd_grpc.StreamD_ProxyPacketsServer,
) error {

}*/

func (grpc *GRPCServer) GetStreamSinkConfig(
	ctx context.Context,
	req *streamd_grpc.GetStreamSinkConfigRequest,
) (*streamd_grpc.GetStreamSinkConfigReply, error) {
	streamID := goconv.StreamIDFullyQualifiedFromGRPC(req.GetStreamSourceID())
	cfg, err := grpc.StreamD.GetStreamSinkConfig(ctx, streamID)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.GetStreamSinkConfigReply{
		Config: &streamd_grpc.StreamSinkConfig{
			Url:            cfg.URL,
			StreamKey:      cfg.StreamKey.Get(),
			StreamSourceID: req.GetStreamSourceID(),
		},
	}, nil
}

func (grpc *GRPCServer) LLMGenerate(
	ctx context.Context,
	req *streamd_grpc.LLMGenerateRequest,
) (*streamd_grpc.LLMGenerateReply, error) {
	response, err := grpc.StreamD.LLMGenerate(
		ctx,
		req.GetPrompt(),
	)
	if err != nil {
		return nil, err
	}
	return &streamd_grpc.LLMGenerateReply{
		Response: response,
	}, nil
}
