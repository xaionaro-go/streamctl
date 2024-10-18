package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/expression"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func ActionGRPC2Go(
	actionGRPC *streamd_grpc.Action,
) (action.Action, error) {
	var result action.Action
	switch a := actionGRPC.ActionOneof.(type) {
	case *streamd_grpc.Action_NoopRequest:
		result = &action.Noop{}
	case *streamd_grpc.Action_StartStreamRequest:
		platID := streamcontrol.PlatformName(a.StartStreamRequest.PlatID)
		profile, err := ProfileGRPC2Go(platID, a.StartStreamRequest.GetProfile())
		if err != nil {
			return nil, err
		}
		result = &action.StartStream{
			PlatID:      platID,
			Title:       a.StartStreamRequest.Title,
			Description: a.StartStreamRequest.Description,
			Profile:     profile,
			CustomArgs:  nil,
		}
	case *streamd_grpc.Action_EndStreamRequest:
		result = &action.EndStream{
			PlatID: streamcontrol.PlatformName(a.EndStreamRequest.PlatID),
		}
	case *streamd_grpc.Action_ObsAction:
		switch o := a.ObsAction.OBSActionOneOf.(type) {
		case *streamd_grpc.OBSAction_ItemShowHide:
			result = &action.OBSItemShowHide{
				ItemName:        o.ItemShowHide.ItemName,
				ItemUUID:        o.ItemShowHide.ItemUUID,
				ValueExpression: expression.Expression(o.ItemShowHide.ValueExpression),
			}
		case *streamd_grpc.OBSAction_WindowCaptureSetSource:
			result = &action.OBSWindowCaptureSetSource{
				ItemName:        o.WindowCaptureSetSource.ItemName,
				ItemUUID:        o.WindowCaptureSetSource.ItemUUID,
				ValueExpression: expression.Expression(o.WindowCaptureSetSource.ValueExpression),
			}
		default:
			return nil, fmt.Errorf("unexpected OBS action type: %T", o)
		}
	default:
		return nil, fmt.Errorf("unexpected action type: %T", a)
	}
	return result, nil
}

func ActionGo2GRPC(
	input action.Action,
) (*streamd_grpc.Action, error) {
	result := streamd_grpc.Action{}
	switch a := input.(type) {
	case *action.Noop:
		result.ActionOneof = &streamd_grpc.Action_NoopRequest{}
	case *action.StartStream:
		profileString, err := ProfileGo2GRPC(a.Profile)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize the profile: %w", err)
		}
		result.ActionOneof = &streamd_grpc.Action_StartStreamRequest{
			StartStreamRequest: &streamd_grpc.StartStreamRequest{
				PlatID:      string(a.PlatID),
				Title:       a.Title,
				Description: a.Description,
				Profile:     profileString,
			},
		}
	case *action.EndStream:
		result.ActionOneof = &streamd_grpc.Action_EndStreamRequest{
			EndStreamRequest: &streamd_grpc.EndStreamRequest{
				PlatID: string(a.PlatID),
			},
		}
	case *action.OBSItemShowHide:
		result.ActionOneof = &streamd_grpc.Action_ObsAction{
			ObsAction: &streamd_grpc.OBSAction{
				OBSActionOneOf: &streamd_grpc.OBSAction_ItemShowHide{
					ItemShowHide: &streamd_grpc.OBSActionItemShowHide{
						ItemName:        a.ItemName,
						ItemUUID:        a.ItemUUID,
						ValueExpression: string(a.ValueExpression),
					},
				},
			},
		}
	case *action.OBSWindowCaptureSetSource:
		result.ActionOneof = &streamd_grpc.Action_ObsAction{
			ObsAction: &streamd_grpc.OBSAction{
				OBSActionOneOf: &streamd_grpc.OBSAction_WindowCaptureSetSource{
					WindowCaptureSetSource: &streamd_grpc.OBSActionWindowCaptureSetSource{
						ItemName:        a.ItemName,
						ItemUUID:        a.ItemUUID,
						ValueExpression: string(a.ValueExpression),
					},
				},
			},
		}
	default:
		return nil, fmt.Errorf("unknown action type: %T", a)
	}
	return &result, nil
}
