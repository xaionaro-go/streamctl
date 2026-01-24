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
	case *streamd_grpc.Action_SetStreamActiveRequest:
		result = &action.SetStreamActive{
			StreamID: StreamIDFullyQualifiedFromGRPC(a.SetStreamActiveRequest.Id),
			IsActive: a.SetStreamActiveRequest.IsActive,
		}
	case *streamd_grpc.Action_SetTitleRequest:
		result = &action.SetTitle{
			StreamID: StreamIDFullyQualifiedFromGRPC(a.SetTitleRequest.Id),
			Title:    a.SetTitleRequest.Title,
		}
	case *streamd_grpc.Action_SetDescriptionRequest:
		result = &action.SetDescription{
			StreamID:    StreamIDFullyQualifiedFromGRPC(a.SetDescriptionRequest.Id),
			Description: a.SetDescriptionRequest.Description,
		}
	case *streamd_grpc.Action_ApplyProfileRequest:
		result = &action.ApplyProfile{
			StreamID: StreamIDFullyQualifiedFromGRPC(a.ApplyProfileRequest.Id),
			Profile:  streamcontrol.ProfileName(a.ApplyProfileRequest.Profile),
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
	case *action.SetStreamActive:
		result.ActionOneof = &streamd_grpc.Action_SetStreamActiveRequest{
			SetStreamActiveRequest: &streamd_grpc.SetStreamActiveRequest{
				Id:       StreamIDFullyQualifiedToGRPC(a.StreamID),
				IsActive: a.IsActive,
			},
		}
	case *action.SetTitle:
		result.ActionOneof = &streamd_grpc.Action_SetTitleRequest{
			SetTitleRequest: &streamd_grpc.SetTitleRequest{
				Id:    StreamIDFullyQualifiedToGRPC(a.StreamID),
				Title: a.Title,
			},
		}
	case *action.SetDescription:
		result.ActionOneof = &streamd_grpc.Action_SetDescriptionRequest{
			SetDescriptionRequest: &streamd_grpc.SetDescriptionRequest{
				Id:          StreamIDFullyQualifiedToGRPC(a.StreamID),
				Description: a.Description,
			},
		}
	case *action.ApplyProfile:
		result.ActionOneof = &streamd_grpc.Action_ApplyProfileRequest{
			ApplyProfileRequest: &streamd_grpc.ApplyProfileRequest{
				Id:      StreamIDFullyQualifiedToGRPC(a.StreamID),
				Profile: string(a.Profile),
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
		return nil, fmt.Errorf("unexpected action type: %T", a)
	}
	return &result, nil
}
