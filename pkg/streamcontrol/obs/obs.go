package obs

import (
	"context"
	"fmt"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/andreykaipov/goobs/api/requests/scenes"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

type OBS struct {
	Config        Config
	CurrentStream struct {
		EnableRecording bool
	}
	IsClosed bool
}

var _ streamcontrol.StreamController[StreamProfile] = (*OBS)(nil)

func New(
	ctx context.Context,
	cfg Config,
) (*OBS, error) {
	if cfg.Config.Host == "" {
		return nil, fmt.Errorf("'host' is not set")
	}
	if cfg.Config.Port == 0 {
		return nil, fmt.Errorf("'port' is not set")
	}

	return &OBS{
		Config: cfg,
	}, nil
}

type GetClientOption goobs.Option

func (obs *OBS) GetClient(clientOpts ...GetClientOption) (*goobs.Client, error) {
	if obs.IsClosed {
		return nil, fmt.Errorf("closed")
	}
	var opts []goobs.Option
	for _, opt := range clientOpts {
		opts = append(opts, goobs.Option(opt))
	}
	if obs.Config.Config.Password.Get() != "" {
		opts = append(opts, goobs.WithPassword(obs.Config.Config.Password.Get()))
	}
	return goobs.New(
		fmt.Sprintf("%s:%d", obs.Config.Config.Host, obs.Config.Config.Port),
		opts...,
	)
}

func (obs *OBS) Close() error {
	obs.IsClosed = true
	return nil
}

func (obs *OBS) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) error {
	return fmt.Errorf("not supported")
}

func (obs *OBS) SetTitle(
	ctx context.Context,
	title string,
) error {
	// So nothing to do here:
	return nil
}

func (obs *OBS) SetDescription(
	ctx context.Context,
	description string,
) error {
	// So nothing to do here:
	return nil
}

func (obs *OBS) InsertAdsCuePoint(
	ctx context.Context,
	ts time.Time,
	duration time.Duration,
) error {
	// So nothing to do here:
	return nil
}

func (obs *OBS) Flush(
	ctx context.Context,
) error {
	// So nothing to do here:
	return nil
}

func (obs *OBS) StartStream(
	ctx context.Context,
	title string,
	description string,
	profile StreamProfile,
	customArgs ...any,
) error {
	client, err := obs.GetClient()
	if err != nil {
		return fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	streamStatus, err := client.Stream.GetStreamStatus()
	if err != nil {
		return fmt.Errorf("unable to get current stream status: %w", err)
	}

	recordingStarted := false
	if profile.EnableRecording {
		recordStatus, err := client.Record.GetRecordStatus()
		if err != nil {
			return fmt.Errorf("unable to get current recording status: %w", err)
		}

		if !recordStatus.OutputActive {
			_, recordStartErr := client.Record.StartRecord()
			if recordStartErr == nil {
				recordingStarted = true
			} else {
				err = multierror.Append(err, recordStartErr)
			}
		}
	}

	if !streamStatus.OutputActive {
		_, streamStartErr := client.Stream.StartStream()
		if streamStartErr != nil {
			err = multierror.Append(err, streamStartErr)
		}
	}

	if err != nil {
		if recordingStarted {
			_, e0 := client.Record.StopRecord()
			logger.Debugf(ctx, "StopRecord result: %v", e0)
		}
		_, e1 := client.Stream.StopStream()
		logger.Debugf(ctx, "StopStream result: %v", e1)

		return err
	}

	obs.CurrentStream.EnableRecording = profile.EnableRecording
	return nil
}

func (obs *OBS) EndStream(
	ctx context.Context,
) error {
	client, err := obs.GetClient()
	if err != nil {
		return fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	streamStatus, err := client.Stream.GetStreamStatus()
	if err != nil {
		return fmt.Errorf("unable to get current stream status: %w", err)
	}

	if obs.CurrentStream.EnableRecording {
		recordStatus, err := client.Record.GetRecordStatus()
		if err != nil {
			return fmt.Errorf("unable to get current recording status: %w", err)
		}

		if recordStatus.OutputActive {
			_, recordStopErr := client.Record.StopRecord()
			if recordStopErr != nil {
				err = multierror.Append(err, recordStopErr)
			}
		}
	}

	if streamStatus.OutputActive {
		_, streamStopErr := client.Stream.StopStream()
		if streamStopErr != nil {
			err = multierror.Append(err, streamStopErr)
		}
	}

	return err
}

func (obs *OBS) GetStreamStatus(
	ctx context.Context,
) (*streamcontrol.StreamStatus, error) {
	client, err := obs.GetClient()
	if err != nil {
		return nil, fmt.Errorf("unable to initialize client to OBS: %w", err)
	}
	defer client.Disconnect()

	streamStatus, err := client.Stream.GetStreamStatus()
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

	resp, err := client.Scenes.GetSceneList(&scenes.GetSceneListParams{})
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

	_, err = client.Scenes.SetCurrentProgramScene(req)
	if err != nil {
		return fmt.Errorf("unable to set the scene: %w", err)
	}
	return nil
}

func (obs *OBS) GetChatMessagesChan(
	ctx context.Context,
) (<-chan streamcontrol.ChatMessage, error) {
	return nil, nil
}

func (obs *OBS) SendChatMessage(
	ctx context.Context,
	message string,
) error {
	return fmt.Errorf("not implemented, yet")
}
func (obs *OBS) RemoveChatMessage(
	ctx context.Context,
	messageID streamcontrol.ChatMessageID,
) error {
	return fmt.Errorf("not implemented, yet")
}
func (obs *OBS) BanUser(
	ctx context.Context,
	userID streamcontrol.ChatUserID,
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
