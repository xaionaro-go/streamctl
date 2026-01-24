package obs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type OBS struct {
	Config        AccountConfig
	SaveCfgFn     func(AccountConfig) error
	CurrentStream struct {
		EnableRecording bool
	}
	IsClosed bool
}

var _ streamcontrol.AccountGeneric[StreamProfile] = (*OBS)(nil)

func (o *OBS) String() string {
	return string(ID)
}

func (o *OBS) GetPlatformID() streamcontrol.PlatformID {
	return ID
}

func (obs *OBS) Platform() streamcontrol.PlatformID {
	return ID
}

func New(
	ctx context.Context,
	cfg AccountConfig,
	saveCfgFn func(AccountConfig) error,
) (*OBS, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("'host' is not set")
	}
	if cfg.Port == 0 {
		return nil, fmt.Errorf("'port' is not set")
	}

	return &OBS{
		Config:    cfg,
		SaveCfgFn: saveCfgFn,
	}, nil
}

type GetClientOption goobs.Option

func SetDebugUseMockClient(v bool) {
	debugUseMockClient = v
}

var (
	debugUseMockClient    = false
	debugMockClient       client
	debugMockClientLocker sync.Mutex
)

func (obs *OBS) GetClient(clientOpts ...GetClientOption) (client, error) {
	if obs.IsClosed {
		return nil, fmt.Errorf("closed")
	}
	if debugUseMockClient {
		debugMockClientLocker.Lock()
		defer debugMockClientLocker.Unlock()
		if debugMockClient == nil {
			debugMockClient = newClientMock()
		}
		return debugMockClient, nil
	}
	var opts []goobs.Option
	for _, opt := range clientOpts {
		opts = append(opts, goobs.Option(opt))
	}
	if obs.Config.Password.Get() != "" {
		opts = append(opts, goobs.WithPassword(obs.Config.Password.Get()))
	}
	c, err := goobs.New(
		fmt.Sprintf("%s:%d", obs.Config.Host, obs.Config.Port),
		opts...,
	)
	if err != nil {
		return nil, err
	}
	return &clientGoobs{Client: c}, nil
}

func (obs *OBS) Close() error {
	obs.IsClosed = true
	return nil
}

func (obs *OBS) GetStreams(ctx context.Context) ([]streamcontrol.StreamInfo, error) {
	return []streamcontrol.StreamInfo{{ID: streamcontrol.DefaultStreamID, Name: "default"}}, nil
}

func (obs *OBS) ApplyProfile(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	profile StreamProfile,
	customArgs ...any,
) error {

	obs.CurrentStream.EnableRecording = profile.EnableRecording
	return nil
}

func (obs *OBS) SetTitle(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	title string,
) error {

	// So nothing to do here:
	return nil
}

func (obs *OBS) SetDescription(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	description string,
) error {

	// So nothing to do here:
	return nil
}

func (obs *OBS) InsertAdsCuePoint(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	ts time.Time,
	duration time.Duration,
) error {

	// So nothing to do here:
	return nil
}

func (obs *OBS) Flush(
	ctx context.Context,
	streamID streamcontrol.StreamID,
) error {

	// So nothing to do here:
	return nil
}

func (obs *OBS) SetStreamActive(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	isActive bool,
) error {

	client, err := obs.GetClient()
	if err != nil {
		return fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	streamStatus, err := client.GetStreamStatus()
	if err != nil {
		return fmt.Errorf("unable to get current stream status: %w", err)
	}

	if isActive {
		recordingStarted := false
		if obs.CurrentStream.EnableRecording {
			recordStatus, err := client.GetRecordStatus()
			if err != nil {
				return fmt.Errorf("unable to get current recording status: %w", err)
			}

			if !recordStatus.OutputActive {
				_, recordStartErr := client.StartRecord()
				if recordStartErr == nil {
					recordingStarted = true
				} else {
					err = multierror.Append(err, recordStartErr)
				}
			}
		}

		if !streamStatus.OutputActive {
			_, streamStartErr := client.StartStream()
			if streamStartErr != nil {
				err = multierror.Append(err, streamStartErr)
			}
		}

		if err != nil {
			if recordingStarted {
				_, e0 := client.StopRecord()
				logger.Debugf(ctx, "StopRecord result: %v", e0)
			}
			_, e1 := client.StopStream()
			logger.Debugf(ctx, "StopStream result: %v", e1)

			return err
		}
	} else {
		if obs.CurrentStream.EnableRecording {
			recordStatus, err := client.GetRecordStatus()
			if err != nil {
				return fmt.Errorf("unable to get current recording status: %w", err)
			}

			if recordStatus.OutputActive {
				_, recordStopErr := client.StopRecord()
				if recordStopErr != nil {
					err = multierror.Append(err, recordStopErr)
				}
			}
		}

		if streamStatus.OutputActive {
			_, streamStopErr := client.StopStream()
			if streamStopErr != nil {
				err = multierror.Append(err, streamStopErr)
			}
		}
	}

	return err
}

func (obs *OBS) GetStreamStatus(
	ctx context.Context,
	streamID streamcontrol.StreamID,
) (_ret *streamcontrol.StreamStatus, _err error) {

	logger.Debugf(ctx, "GetStreamStatus")
	defer func() { logger.Debugf(ctx, "/GetStreamStatus: %s %v", spew.Sdump(_ret), _err) }()

	client, err := obs.GetClient()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	streamStatus, err := client.GetStreamStatus()
	if err != nil {
		return nil, fmt.Errorf("unable to get current stream status: %w", err)
	}

	var startedAt *time.Time
	if streamStatus.OutputActive {
		startedAt = ptr(
			time.Now().Add(-time.Duration(streamStatus.OutputDuration * float64(time.Millisecond))),
		)
	}

	return &streamcontrol.StreamStatus{
		IsActive:  streamStatus.OutputActive,
		StartedAt: startedAt,
	}, nil
}

func (obs *OBS) GetSceneList(
	ctx context.Context,
) (*scenes.GetSceneListResponse, error) {
	client, err := obs.GetClient()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	resp, err := client.GetSceneList(&scenes.GetSceneListParams{})
	if err != nil {
		return nil, fmt.Errorf("unable to get the list of scenes: %w", err)
	}

	return resp, nil
}

func (obs *OBS) SetCurrentProgramScene(
	ctx context.Context,
	req *scenes.SetCurrentProgramSceneParams,
) error {
	client, err := obs.GetClient()
	if err != nil {
		return fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	_, err = client.SetCurrentProgramScene(req)
	if err != nil {
		return fmt.Errorf("unable to set the scene: %w", err)
	}
	return nil
}

func (obs *OBS) GetChatMessagesChan(
	ctx context.Context,
	streamID streamcontrol.StreamID,
) (<-chan streamcontrol.Event, error) {
	return nil, nil
}

func (obs *OBS) SendChatMessage(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	message string,
) error {
	return fmt.Errorf("not implemented, yet")
}
func (obs *OBS) RemoveChatMessage(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	messageID streamcontrol.EventID,
) error {
	return fmt.Errorf("not implemented, yet")
}
func (obs *OBS) BanUser(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	userID streamcontrol.UserID,
	reason string,
	deadline time.Time,
) error {
	return fmt.Errorf("not implemented, yet")
}

func (obs *OBS) IsCapable(
	ctx context.Context,
	cap streamcontrol.Capability,
) bool {
	return false
}

func (obs *OBS) IsChannelStreaming(
	ctx context.Context,
	chanID streamcontrol.UserID,
) (bool, error) {
	return false, fmt.Errorf("not implemented")
}

func (obs *OBS) RaidTo(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	chanID streamcontrol.UserID,
) error {
	return fmt.Errorf("not implemented")
}

func (obs *OBS) Shoutout(
	ctx context.Context,
	streamID streamcontrol.StreamID,
	chanID streamcontrol.UserID,
) error {
	return fmt.Errorf("not implemented")
}
