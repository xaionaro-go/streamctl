package client

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"time"

	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/andreykaipov/goobs/api/typedefs"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/player"
	"github.com/xaionaro-go/streamctl/pkg/player/protobuf/go/player_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	obs "github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs/types"
	twitch "github.com/xaionaro-go/streamctl/pkg/streamcontrol/twitch/types"
	youtube "github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streampanel/consts"
	sptypes "github.com/xaionaro-go/streamctl/pkg/streamplayer/types"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	Target string
}

var _ api.StreamD = (*Client)(nil)

func New(target string) *Client {
	return &Client{Target: target}
}

func (c *Client) grpcClient() (streamd_grpc.StreamDClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		c.Target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize a gRPC client: %w", err)
	}

	client := streamd_grpc.NewStreamDClient(conn)
	return client, conn, nil
}

func (c *Client) Run(ctx context.Context) error {
	return nil
}

func (c *Client) FetchConfig(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.OBSOLETE_FetchConfig(ctx, &streamd_grpc.OBSOLETE_FetchConfigRequest{})
	return err
}

func (c *Client) InitCache(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.InitCache(ctx, &streamd_grpc.InitCacheRequest{})
	return err
}

func (c *Client) SaveConfig(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SaveConfig(ctx, &streamd_grpc.SaveConfigRequest{})
	return err
}

func (c *Client) ResetCache(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.ResetCache(ctx, &streamd_grpc.ResetCacheRequest{})
	return err
}

func (c *Client) GetConfig(ctx context.Context) (*config.Config, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.GetConfig(ctx, &streamd_grpc.GetConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request the config: %w", err)
	}

	var result config.Config
	_, err = result.Read([]byte(reply.Config))
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize the received config: %w", err)
	}
	return &result, nil
}

func (c *Client) SetConfig(ctx context.Context, cfg *config.Config) error {
	var buf bytes.Buffer
	_, err := cfg.WriteTo(&buf)
	if err != nil {
		return fmt.Errorf("unable to serialize the config: %w", err)
	}

	req := &streamd_grpc.SetConfigRequest{
		Config: buf.String(),
	}

	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetConfig(ctx, req)
	return err
}

func (c *Client) IsBackendEnabled(ctx context.Context, id streamcontrol.PlatformName) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	reply, err := client.IsBackendEnabled(ctx, &streamd_grpc.IsBackendEnabledRequest{
		PlatID: string(id),
	})
	if err != nil {
		return false, fmt.Errorf("unable to get backend info: %w", err)
	}
	return reply.IsInitialized, nil
}

func (c *Client) OBSOLETE_IsGITInitialized(ctx context.Context) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	reply, err := client.OBSOLETE_GitInfo(ctx, &streamd_grpc.OBSOLETE_GetGitInfoRequest{})
	if err != nil {
		return false, fmt.Errorf("unable to get backend info: %w", err)
	}
	return reply.IsInitialized, nil
}

func (c *Client) StartStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.StartStream(ctx, &streamd_grpc.StartStreamRequest{
		PlatID:      string(platID),
		Title:       title,
		Description: description,
		Profile:     string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to start the stream: %w", err)
	}

	return nil
}
func (c *Client) EndStream(ctx context.Context, platID streamcontrol.PlatformName) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.EndStream(ctx, &streamd_grpc.EndStreamRequest{PlatID: string(platID)})
	if err != nil {
		return fmt.Errorf("unable to end the stream: %w", err)
	}

	return nil
}

func (c *Client) OBSOLETE_GitRelogin(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.OBSOLETE_GitRelogin(ctx, &streamd_grpc.OBSOLETE_GitReloginRequest{})
	if err != nil {
		return fmt.Errorf("unable force git relogin: %w", err)
	}

	return nil
}

func (c *Client) GetBackendData(ctx context.Context, platID streamcontrol.PlatformName) (any, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.GetBackendInfo(ctx, &streamd_grpc.GetBackendInfoRequest{
		PlatID: string(platID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get backend info: %w", err)
	}

	var data any
	switch platID {
	case obs.ID:
		_data := api.BackendDataOBS{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	case twitch.ID:
		_data := api.BackendDataTwitch{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	case youtube.ID:
		_data := api.BackendDataYouTube{}
		err = json.Unmarshal([]byte(reply.GetData()), &_data)
		data = _data
	default:
		return nil, fmt.Errorf("unknown platform: '%s'", platID)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to deserialize data: %w", err)
	}
	return data, nil
}

func (c *Client) Restart(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.Restart(ctx, &streamd_grpc.RestartRequest{})
	if err != nil {
		return fmt.Errorf("unable restart the server: %w", err)
	}

	return nil
}

func (c *Client) EXPERIMENTAL_ReinitStreamControllers(ctx context.Context) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.EXPERIMENTAL_ReinitStreamControllers(ctx, &streamd_grpc.EXPERIMENTAL_ReinitStreamControllersRequest{})
	if err != nil {
		return fmt.Errorf("unable restart the server: %w", err)
	}

	return nil

}

func (c *Client) GetStreamStatus(
	ctx context.Context,
	platID streamcontrol.PlatformName,
) (*streamcontrol.StreamStatus, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	streamStatus, err := client.GetStreamStatus(ctx, &streamd_grpc.GetStreamStatusRequest{
		PlatID: string(platID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get the stream status of '%s': %w", platID, err)
	}

	var startedAt *time.Time
	if streamStatus != nil && streamStatus.StartedAt != nil {
		v := *streamStatus.StartedAt
		startedAt = ptr(time.Unix(v/1000000000, v%1000000000))
	}

	var customData any
	switch platID {
	case youtube.ID:
		d := youtube.StreamStatusCustomData{}
		err := json.Unmarshal([]byte(streamStatus.GetCustomData()), &d)
		if err != nil {
			return nil, fmt.Errorf("unable to unserialize the custom data: %w", err)
		}
		customData = d
	}

	return &streamcontrol.StreamStatus{
		IsActive:   streamStatus.GetIsActive(),
		StartedAt:  startedAt,
		CustomData: customData,
	}, nil
}

func (c *Client) SetTitle(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetTitle(ctx, &streamd_grpc.SetTitleRequest{
		PlatID: string(platID),
		Title:  title,
	})
	return err
}
func (c *Client) SetDescription(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	description string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetDescription(ctx, &streamd_grpc.SetDescriptionRequest{
		PlatID:      string(platID),
		Description: description,
	})
	return err
}
func (c *Client) ApplyProfile(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.ApplyProfile(ctx, &streamd_grpc.ApplyProfileRequest{
		PlatID:  string(platID),
		Profile: string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to apply the profile to the stream: %w", err)
	}

	return nil
}

func (c *Client) UpdateStream(
	ctx context.Context,
	platID streamcontrol.PlatformName,
	title string, description string,
	profile streamcontrol.AbstractStreamProfile,
	customArgs ...any,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	b, err := yaml.Marshal(profile)
	if err != nil {
		return fmt.Errorf("unable to serialize the profile: %w", err)
	}

	logger.Debugf(ctx, "serialized profile: '%s'", profile)

	_, err = client.UpdateStream(ctx, &streamd_grpc.UpdateStreamRequest{
		PlatID:      string(platID),
		Title:       title,
		Description: description,
		Profile:     string(b),
	})
	if err != nil {
		return fmt.Errorf("unable to update the stream: %w", err)
	}

	return nil
}

func (c *Client) SubscriberToOAuthURLs(
	ctx context.Context,
	listenPort uint16,
) (chan *streamd_grpc.OAuthRequest, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	result := make(chan *streamd_grpc.OAuthRequest)

	subClient, err := client.SubscribeToOAuthRequests(ctx, &streamd_grpc.SubscribeToOAuthRequestsRequest{
		ListenPort: int32(listenPort),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe to oauth URLs: %w", err)
	}
	subClient.CloseSend()
	observability.Go(ctx, func() {
		defer conn.Close()
		defer func() {
			close(result)
		}()

		for {
			res, err := subClient.Recv()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err == io.EOF {
				logger.Debugf(ctx, "the receiver is closed: %v", err)
				return
			}
			if err != nil {
				logger.Errorf(ctx, "unable to read data: %v", err)
				return
			}

			result <- res
		}
	})

	return result, nil
}

func (c *Client) GetVariable(
	ctx context.Context,
	key consts.VarKey,
) ([]byte, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.GetVariable(ctx, &streamd_grpc.GetVariableRequest{Key: string(key)})
	if err != nil {
		return nil, fmt.Errorf("unable to get the variable '%s' value: %w", key, err)
	}

	b := reply.GetValue()
	logger.Tracef(ctx, "downloaded variable value of size %d", len(b))
	return b, nil
}

func (c *Client) GetVariableHash(
	ctx context.Context,
	key consts.VarKey,
	hashType crypto.Hash,
) ([]byte, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var hashTypeArg streamd_grpc.HashType
	switch hashType {
	case crypto.SHA1:
		hashTypeArg = streamd_grpc.HashType_HASH_SHA1
	default:
		return nil, fmt.Errorf("unsupported hash type: %s", hashType)
	}

	reply, err := client.GetVariableHash(ctx, &streamd_grpc.GetVariableHashRequest{
		Key:      string(key),
		HashType: hashTypeArg,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get the variable '%s' hash: %w", key, err)
	}

	b := reply.GetHash()
	logger.Tracef(ctx, "the downloaded hash of the variable '%s' is %X", key, b)
	return b, nil
}

func (c *Client) SetVariable(
	ctx context.Context,
	key consts.VarKey,
	value []byte,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.SetVariable(ctx, &streamd_grpc.SetVariableRequest{
		Key:   string(key),
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("unable to get the variable '%s' value: %w", key, err)
	}

	return nil
}

func (c *Client) OBSGetSceneList(
	ctx context.Context,
) (*scenes.GetSceneListResponse, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.OBSGetSceneList(ctx, &streamd_grpc.OBSGetSceneListRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of OBS scenes: %w", err)
	}

	result := &scenes.GetSceneListResponse{
		CurrentPreviewSceneName: resp.CurrentPreviewSceneName,
		CurrentPreviewSceneUuid: resp.CurrentPreviewSceneUUID,
		CurrentProgramSceneName: resp.CurrentProgramSceneName,
		CurrentProgramSceneUuid: resp.CurrentProgramSceneUUID,
	}
	for _, scene := range resp.Scenes {
		result.Scenes = append(result.Scenes, &typedefs.Scene{
			SceneUuid:  scene.Uuid,
			SceneIndex: int(scene.Index),
			SceneName:  scene.Name,
		})
	}

	return result, nil
}
func (c *Client) OBSSetCurrentProgramScene(
	ctx context.Context,
	in *scenes.SetCurrentProgramSceneParams,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	req := &streamd_grpc.OBSSetCurrentProgramSceneRequest{}
	switch {
	case in.SceneUuid != nil:
		req.OBSSceneID = &streamd_grpc.OBSSetCurrentProgramSceneRequest_SceneUUID{
			SceneUUID: *in.SceneUuid,
		}
	case in.SceneName != nil:
		req.OBSSceneID = &streamd_grpc.OBSSetCurrentProgramSceneRequest_SceneName{
			SceneName: *in.SceneName,
		}
	}
	_, err = client.OBSSetCurrentProgramScene(ctx, req)
	if err != nil {
		return fmt.Errorf("unable to set the program scene in OBS: %w", err)
	}
	return nil
}

func ptr[T any](in T) *T {
	return &in
}

func (c *Client) SubmitOAuthCode(
	ctx context.Context,
	req *streamd_grpc.SubmitOAuthCodeRequest,
) (*streamd_grpc.SubmitOAuthCodeReply, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	return client.SubmitOAuthCode(ctx, req)
}

func (c *Client) ListStreamServers(
	ctx context.Context,
) ([]api.StreamServer, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reply, err := client.ListStreamServers(
		ctx,
		&streamd_grpc.ListStreamServersRequest{},
	)
	if err != nil {
		return nil, fmt.Errorf("unable to request to list of the stream servers: %w", err)
	}
	var result []api.StreamServer
	for _, server := range reply.GetStreamServers() {
		t, err := goconv.StreamServerTypeGRPC2Go(server.Config.GetServerType())
		if err != nil {
			return nil, fmt.Errorf("unable to convert the server type value: %w", err)
		}
		result = append(result, api.StreamServer{
			Type:                  t,
			ListenAddr:            server.Config.GetListenAddr(),
			NumBytesConsumerWrote: uint64(server.GetStatistics().GetNumBytesConsumerWrote()),
			NumBytesProducerRead:  uint64(server.GetStatistics().GetNumBytesProducerRead()),
		})
	}
	return result, nil
}

func (c *Client) StartStreamServer(
	ctx context.Context,
	serverType api.StreamServerType,
	listenAddr string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	t, err := goconv.StreamServerTypeGo2GRPC(serverType)
	if err != nil {
		return fmt.Errorf("unable to convert the server type: %w", err)
	}
	_, err = client.StartStreamServer(ctx, &streamd_grpc.StartStreamServerRequest{
		Config: &streamd_grpc.StreamServer{
			ServerType: t,
			ListenAddr: listenAddr,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to start the stream server: %w", err)
	}
	return nil
}

func (c *Client) StopStreamServer(
	ctx context.Context,
	listenAddr string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StopStreamServer(ctx, &streamd_grpc.StopStreamServerRequest{
		ListenAddr: listenAddr,
	})
	if err != nil {
		return fmt.Errorf("unable to request to stop the stream server: %w", err)
	}
	return nil
}

func (c *Client) AddIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.AddIncomingStream(ctx, &streamd_grpc.AddIncomingStreamRequest{
		StreamID: string(streamID),
	})
	if err != nil {
		return fmt.Errorf("unable to request to add the incoming stream: %w", err)
	}
	return nil
}

func (c *Client) RemoveIncomingStream(
	ctx context.Context,
	streamID api.StreamID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.RemoveIncomingStream(ctx, &streamd_grpc.RemoveIncomingStreamRequest{
		StreamID: string(streamID),
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the incoming stream: %w", err)
	}
	return nil
}

func (c *Client) ListIncomingStreams(
	ctx context.Context,
) ([]api.IncomingStream, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.ListIncomingStreams(ctx, &streamd_grpc.ListIncomingStreamsRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request to list the incoming streams: %w", err)
	}

	var result []api.IncomingStream
	for _, stream := range reply.GetIncomingStreams() {
		result = append(result, api.IncomingStream{
			StreamID: api.StreamID(stream.GetStreamID()),
		})
	}
	return result, nil
}

func (c *Client) ListStreamDestinations(
	ctx context.Context,
) ([]api.StreamDestination, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.ListStreamDestinations(ctx, &streamd_grpc.ListStreamDestinationsRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request to list the stream destinations: %w", err)
	}

	var result []api.StreamDestination
	for _, dst := range reply.GetStreamDestinations() {
		result = append(result, api.StreamDestination{
			ID:  api.DestinationID(dst.GetDestinationID()),
			URL: dst.GetUrl(),
		})
	}
	return result, nil
}

func (c *Client) AddStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
	url string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.AddStreamDestination(ctx, &streamd_grpc.AddStreamDestinationRequest{
		Config: &streamd_grpc.StreamDestination{
			DestinationID: string(destinationID),
			Url:           url,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to add the stream destination: %w", err)
	}
	return nil
}

func (c *Client) RemoveStreamDestination(
	ctx context.Context,
	destinationID api.DestinationID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.RemoveStreamDestination(ctx, &streamd_grpc.RemoveStreamDestinationRequest{
		DestinationID: string(destinationID),
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the stream destination: %w", err)
	}
	return nil
}

func (c *Client) ListStreamForwards(
	ctx context.Context,
) ([]api.StreamForward, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	reply, err := client.ListStreamForwards(ctx, &streamd_grpc.ListStreamForwardsRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to request to list the stream forwards: %w", err)
	}

	var result []api.StreamForward
	for _, forward := range reply.GetStreamForwards() {
		result = append(result, api.StreamForward{
			Enabled:       forward.Config.Enabled,
			StreamID:      api.StreamID(forward.Config.GetStreamID()),
			DestinationID: api.DestinationID(forward.Config.GetDestinationID()),
			NumBytesWrote: uint64(forward.Statistics.NumBytesWrote),
			NumBytesRead:  uint64(forward.Statistics.NumBytesRead),
		})
	}
	return result, nil
}

func (c *Client) AddStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.AddStreamForward(ctx, &streamd_grpc.AddStreamForwardRequest{
		Config: &streamd_grpc.StreamForward{
			StreamID:      string(streamID),
			DestinationID: string(destinationID),
			Enabled:       enabled,
			Quirks: &streamd_grpc.StreamForwardQuirks{
				RestartUntilYoutubeRecognizesStream: &streamd_grpc.RestartUntilYoutubeRecognizesStream{
					Enabled:        quirks.RestartUntilYoutubeRecognizesStream.Enabled,
					StartTimeout:   quirks.RestartUntilYoutubeRecognizesStream.StartTimeout.Seconds(),
					StopStartDelay: quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay.Seconds(),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to add the stream forward: %w", err)
	}
	return nil
}

func (c *Client) UpdateStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
	enabled bool,
	quirks api.StreamForwardingQuirks,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.UpdateStreamForward(ctx, &streamd_grpc.UpdateStreamForwardRequest{
		Config: &streamd_grpc.StreamForward{
			StreamID:      string(streamID),
			DestinationID: string(destinationID),
			Enabled:       enabled,
			Quirks: &streamd_grpc.StreamForwardQuirks{
				RestartUntilYoutubeRecognizesStream: &streamd_grpc.RestartUntilYoutubeRecognizesStream{
					Enabled:        quirks.RestartUntilYoutubeRecognizesStream.Enabled,
					StartTimeout:   quirks.RestartUntilYoutubeRecognizesStream.StartTimeout.Seconds(),
					StopStartDelay: quirks.RestartUntilYoutubeRecognizesStream.StopStartDelay.Seconds(),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to add the stream forward: %w", err)
	}
	return nil
}

func (c *Client) RemoveStreamForward(
	ctx context.Context,
	streamID api.StreamID,
	destinationID api.DestinationID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.RemoveStreamForward(ctx, &streamd_grpc.RemoveStreamForwardRequest{
		Config: &streamd_grpc.StreamForward{
			StreamID:      string(streamID),
			DestinationID: string(destinationID),
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the stream forward: %w", err)
	}
	return nil
}

func (c *Client) WaitForStreamPublisher(
	ctx context.Context,
	streamID api.StreamID,
) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	waiter, err := client.WaitForStreamPublisher(ctx, &streamd_grpc.WaitForStreamPublisherRequest{
		StreamID: ptr(string(streamID)),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get a publisher waiter for stream '%s': %w", streamID, err)
	}

	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		conn.Close()
	})

	result := make(chan struct{})
	waiter.CloseSend()
	observability.Go(ctx, func() {
		defer cancelFn()
		defer conn.Close()
		defer func() {
			close(result)
		}()

		_, err := waiter.Recv()
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err == io.EOF {
			logger.Debugf(ctx, "the receiver is closed: %v", err)
			return
		}
		if err != nil {
			logger.Errorf(ctx, "unable to read data: %v", err)
			return
		}
	})

	return result, nil
}

func (c *Client) AddStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.AddStreamPlayer(ctx, &streamd_grpc.AddStreamPlayerRequest{
		Config: &streamd_grpc.StreamPlayerConfig{
			StreamID:             string(streamID),
			PlayerType:           goconv.StreamPlayerTypeGo2GRPC(playerType),
			Disabled:             disabled,
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(&streamPlaybackConfig),
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the stream forward: %w", err)
	}
	return nil
}

func (c *Client) UpdateStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
	playerType player.Backend,
	disabled bool,
	streamPlaybackConfig sptypes.Config,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.UpdateStreamPlayer(ctx, &streamd_grpc.UpdateStreamPlayerRequest{
		Config: &streamd_grpc.StreamPlayerConfig{
			StreamID:             string(streamID),
			PlayerType:           goconv.StreamPlayerTypeGo2GRPC(playerType),
			Disabled:             disabled,
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGo2GRPC(&streamPlaybackConfig),
		},
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the stream forward: %w", err)
	}
	return nil
}

func (c *Client) RemoveStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.RemoveStreamPlayer(ctx, &streamd_grpc.RemoveStreamPlayerRequest{
		StreamID: string(streamID),
	})
	if err != nil {
		return fmt.Errorf("unable to request to remove the stream forward: %w", err)
	}
	return nil
}

func (c *Client) ListStreamPlayers(
	ctx context.Context,
) ([]api.StreamPlayer, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.ListStreamPlayers(ctx, &streamd_grpc.ListStreamPlayersRequest{})
	if err != nil {
		return nil, fmt.Errorf("unable to query: %w", err)
	}

	result := make([]api.StreamPlayer, 0, len(resp.GetPlayers()))
	for _, player := range resp.GetPlayers() {
		result = append(result, api.StreamPlayer{
			StreamID:             streamtypes.StreamID(player.GetStreamID()),
			PlayerType:           goconv.StreamPlayerTypeGRPC2Go(player.PlayerType),
			Disabled:             player.GetDisabled(),
			StreamPlaybackConfig: goconv.StreamPlaybackConfigGRPC2Go(player.GetStreamPlaybackConfig()),
		})
	}
	return result, nil
}

func (c *Client) GetStreamPlayer(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (*api.StreamPlayer, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	resp, err := client.GetStreamPlayer(ctx, &streamd_grpc.GetStreamPlayerRequest{
		StreamID: string(streamID),
	})
	if err != nil {
		return nil, fmt.Errorf("unable to query: %w", err)
	}

	cfg := resp.GetConfig()
	return &api.StreamPlayer{
		StreamID:             streamID,
		PlayerType:           goconv.StreamPlayerTypeGRPC2Go(cfg.PlayerType),
		Disabled:             cfg.GetDisabled(),
		StreamPlaybackConfig: goconv.StreamPlaybackConfigGRPC2Go(cfg.StreamPlaybackConfig),
	}, nil
}

func (c *Client) StreamPlayerProcessTitle(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	resp, err := client.StreamPlayerProcessTitle(ctx, &streamd_grpc.StreamPlayerProcessTitleRequest{
		StreamID: string(streamID),
	})
	if err != nil {
		return "", fmt.Errorf("unable to query: %w", err)
	}
	return resp.Reply.GetTitle(), nil
}

func (c *Client) StreamPlayerOpenURL(
	ctx context.Context,
	streamID streamtypes.StreamID,
	link string,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StreamPlayerOpen(ctx, &streamd_grpc.StreamPlayerOpenRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.OpenRequest{},
	})
	if err != nil {
		return fmt.Errorf("unable to query: %w", err)
	}
	return nil
}

func (c *Client) StreamPlayerGetLink(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (string, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	resp, err := client.StreamPlayerGetLink(ctx, &streamd_grpc.StreamPlayerGetLinkRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.GetLinkRequest{},
	})
	if err != nil {
		return "", fmt.Errorf("unable to query: %w", err)
	}
	return resp.GetReply().Link, nil
}

func (c *Client) StreamPlayerEndChan(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (<-chan struct{}, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		conn.Close()
	})

	waiter, err := client.StreamPlayerEndChan(ctx, &streamd_grpc.StreamPlayerEndChanRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.EndChanRequest{},
	})
	if err != nil {
		cancelFn()
		return nil, fmt.Errorf("unable to query: %w", err)
	}
	result := make(chan struct{})
	waiter.CloseSend()
	observability.Go(ctx, func() {
		defer cancelFn()
		defer func() {
			close(result)
		}()

		_, err := waiter.Recv()
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err == io.EOF {
			logger.Debugf(ctx, "the receiver is closed: %v", err)
			return
		}
		if err != nil {
			logger.Errorf(ctx, "unable to read data: %v", err)
			return
		}
	})

	return result, nil
}

func (c *Client) StreamPlayerIsEnded(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (bool, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return false, err
	}
	defer conn.Close()

	resp, err := client.StreamPlayerIsEnded(ctx, &streamd_grpc.StreamPlayerIsEndedRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.IsEndedRequest{},
	})
	if err != nil {
		return false, fmt.Errorf("unable to query: %w", err)
	}
	return resp.GetReply().IsEnded, nil
}

func (c *Client) StreamPlayerGetPosition(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.StreamPlayerGetPosition(ctx, &streamd_grpc.StreamPlayerGetPositionRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.GetPositionRequest{},
	})
	if err != nil {
		return 0, fmt.Errorf("unable to query: %w", err)
	}
	return time.Duration(float64(time.Second) * resp.GetReply().GetPositionSecs()), nil
}

func (c *Client) StreamPlayerGetLength(
	ctx context.Context,
	streamID streamtypes.StreamID,
) (time.Duration, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	resp, err := client.StreamPlayerGetLength(ctx, &streamd_grpc.StreamPlayerGetLengthRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.GetLengthRequest{},
	})
	if err != nil {
		return 0, fmt.Errorf("unable to query: %w", err)
	}
	return time.Duration(float64(time.Second) * resp.GetReply().GetLengthSecs()), nil
}

func (c *Client) StreamPlayerSetSpeed(
	ctx context.Context,
	streamID streamtypes.StreamID,
	speed float64,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StreamPlayerSetSpeed(ctx, &streamd_grpc.StreamPlayerSetSpeedRequest{
		StreamID: string(streamID),
		Request: &player_grpc.SetSpeedRequest{
			Speed: speed,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to query: %w", err)
	}
	return nil
}

func (c *Client) StreamPlayerSetPause(
	ctx context.Context,
	streamID streamtypes.StreamID,
	pause bool,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StreamPlayerSetPause(ctx, &streamd_grpc.StreamPlayerSetPauseRequest{
		StreamID: string(streamID),
		Request: &player_grpc.SetPauseRequest{
			SetPaused: pause,
		},
	})
	if err != nil {
		return fmt.Errorf("unable to query: %w", err)
	}
	return nil
}

func (c *Client) StreamPlayerStop(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StreamPlayerStop(ctx, &streamd_grpc.StreamPlayerStopRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.StopRequest{},
	})
	if err != nil {
		return fmt.Errorf("unable to query: %w", err)
	}
	return nil
}

func (c *Client) StreamPlayerClose(
	ctx context.Context,
	streamID streamtypes.StreamID,
) error {
	client, conn, err := c.grpcClient()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = client.StreamPlayerClose(ctx, &streamd_grpc.StreamPlayerCloseRequest{
		StreamID: string(streamID),
		Request:  &player_grpc.CloseRequest{},
	})
	if err != nil {
		return fmt.Errorf("unable to query: %w", err)
	}
	return nil
}

type receiver[T any] interface {
	grpc.ClientStream

	Recv() (*T, error)
}

func unwrapChan[E any, R any, S receiver[R]](
	ctx context.Context,
	c *Client,
	fn func(ctx context.Context, client streamd_grpc.StreamDClient) (S, error),
) (<-chan E, error) {
	client, conn, err := c.grpcClient()
	if err != nil {
		return nil, err
	}

	pc, _, _, _ := runtime.Caller(1)
	caller := runtime.FuncForPC(pc)
	ctx = belt.WithField(ctx, "func", caller.Name())

	ctx, cancelFn := context.WithCancel(ctx)
	sub, err := fn(ctx, client)
	if err != nil {
		cancelFn()
		return nil, fmt.Errorf("unable to subscribe: %w", err)
	}

	r := make(chan E)
	observability.Go(ctx, func() {
		defer conn.Close()
		defer cancelFn()
		for {
			event, err := sub.Recv()
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err != nil {
				switch {
				case errors.Is(err, io.EOF):
					logger.Debugf(ctx, "the receiver is closed: %v", err)
					return
				case strings.Contains(err.Error(), grpc.ErrClientConnClosing.Error()):
					logger.Debugf(ctx, "apparently we are closing the client: %v", err)
					return
				case strings.Contains(err.Error(), context.Canceled.Error()):
					logger.Debugf(ctx, "subscription was cancelled: %v", err)
					return
				default:
					logger.Errorf(ctx, "unable to read data: %v", err)
					return
				}
			}

			_ = event
			var eventParsed E
			r <- eventParsed
		}
	})
	return r, nil
}

func (c *Client) SubscribeToConfigChanges(
	ctx context.Context,
) (<-chan api.DiffConfig, error) {
	return unwrapChan[api.DiffConfig](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToConfigChangesClient, error) {
			return client.SubscribeToConfigChanges(
				ctx,
				&streamd_grpc.SubscribeToConfigChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffStreams, error) {
	return unwrapChan[api.DiffStreams](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamsChangesClient, error) {
			return client.SubscribeToStreamsChanges(
				ctx,
				&streamd_grpc.SubscribeToStreamsChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToStreamServersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamServers, error) {
	return unwrapChan[api.DiffStreamServers](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamServersChangesClient, error) {
			return client.SubscribeToStreamServersChanges(
				ctx,
				&streamd_grpc.SubscribeToStreamServersChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToStreamDestinationsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamDestinations, error) {
	return unwrapChan[api.DiffStreamDestinations](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamDestinationsChangesClient, error) {
			return client.SubscribeToStreamDestinationsChanges(
				ctx,
				&streamd_grpc.SubscribeToStreamDestinationsChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToIncomingStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffIncomingStreams, error) {
	return unwrapChan[api.DiffIncomingStreams](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToIncomingStreamsChangesClient, error) {
			return client.SubscribeToIncomingStreamsChanges(
				ctx,
				&streamd_grpc.SubscribeToIncomingStreamsChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToStreamForwardsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamForwards, error) {
	return unwrapChan[api.DiffStreamForwards](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamForwardsChangesClient, error) {
			return client.SubscribeToStreamForwardsChanges(
				ctx,
				&streamd_grpc.SubscribeToStreamForwardsChangesRequest{},
			)
		},
	)
}

func (c *Client) SubscribeToStreamPlayersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamPlayers, error) {
	return unwrapChan[api.DiffStreamPlayers](
		ctx,
		c,
		func(
			ctx context.Context,
			client streamd_grpc.StreamDClient,
		) (streamd_grpc.StreamD_SubscribeToStreamPlayersChangesClient, error) {
			return client.SubscribeToStreamPlayersChanges(
				ctx,
				&streamd_grpc.SubscribeToStreamPlayersChangesRequest{},
			)
		},
	)
}
