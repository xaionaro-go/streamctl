package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func ActionGRPC2Go(actionGRPC *streamd_grpc.Action) (api.TimerAction, error) {
	var action api.TimerAction
	switch actionRaw := actionGRPC.ActionOneof.(type) {
	case *streamd_grpc.Action_NoopRequest:
		action = &api.TimerActionNoop{}
	case *streamd_grpc.Action_StartStreamRequest:
		platID := streamcontrol.PlatformName(actionRaw.StartStreamRequest.PlatID)
		profile, err := ProfileGRPC2Go(platID, actionRaw.StartStreamRequest.GetProfile())
		if err != nil {
			return nil, err
		}
		action = &api.TimerActionStartStream{
			PlatID:      platID,
			Title:       actionRaw.StartStreamRequest.Title,
			Description: actionRaw.StartStreamRequest.Description,
			Profile:     profile,
			CustomArgs:  nil,
		}
	case *streamd_grpc.Action_EndStreamRequest:
		action = &api.TimerActionEndStream{
			PlatID: streamcontrol.PlatformName(actionRaw.EndStreamRequest.PlatID),
		}
	default:
		return nil, fmt.Errorf("unexpected timer action: %T", actionRaw)
	}
	return action, nil
}

func ActionGo2GRPC(action api.TimerAction) (*streamd_grpc.Action, error) {
	resultAction := streamd_grpc.Action{}
	switch action := action.(type) {
	case *api.TimerActionNoop:
		resultAction.ActionOneof = &streamd_grpc.Action_NoopRequest{}
	case *api.TimerActionStartStream:
		profileString, err := ProfileGo2GRPC(action.Profile)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize the profile: %w", err)
		}
		resultAction.ActionOneof = &streamd_grpc.Action_StartStreamRequest{
			StartStreamRequest: &streamd_grpc.StartStreamRequest{
				PlatID:      string(action.PlatID),
				Title:       action.Title,
				Description: action.Description,
				Profile:     profileString,
			},
		}
	case *api.TimerActionEndStream:
		resultAction.ActionOneof = &streamd_grpc.Action_EndStreamRequest{
			EndStreamRequest: &streamd_grpc.EndStreamRequest{
				PlatID: string(action.PlatID),
			},
		}
	default:
		return nil, fmt.Errorf("unknown action type: %T", action)
	}
	return &resultAction, nil
}
