package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func EventGo2GRPC(in event.Event) (*streamd_grpc.Event, error) {
	switch q := in.(type) {
	case *event.WindowFocusChange:
		return &streamd_grpc.Event{
			EventOneOf: &streamd_grpc.Event_WindowFocusChange{
				WindowFocusChange: eventGo2GRPCWindowFocusChange(q),
			},
		}, nil
	case *event.OBSSceneChange:
		return &streamd_grpc.Event{
			EventOneOf: &streamd_grpc.Event_ObsSceneChange{
				ObsSceneChange: eventGo2GRPCOBSSceneChange(q),
			},
		}, nil
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}

func eventGo2GRPCWindowFocusChange(q *event.WindowFocusChange) *streamd_grpc.EventWindowFocusChange {
	return &streamd_grpc.EventWindowFocusChange{
		Host:        q.Host,
		WindowID:    q.WindowID,
		WindowTitle: q.WindowTitle,
		ProcessID:   q.ProcessID,
		ProcessName: q.ProcessName,
		UserID:      q.UserID,
		IsFocused:   q.IsFocused,
	}
}

func eventGo2GRPCOBSSceneChange(q *event.OBSSceneChange) *streamd_grpc.EventOBSSceneChange {
	return &streamd_grpc.EventOBSSceneChange{
		From: q.NameFrom,
		To:   q.NameTo,
	}
}

func EventGRPC2Go(in *streamd_grpc.Event) (config.Event, error) {
	switch q := in.EventOneOf.(type) {
	case *streamd_grpc.Event_WindowFocusChange:
		return eventGRPC2GoWindowFocusChange(q.WindowFocusChange), nil
	case *streamd_grpc.Event_ObsSceneChange:
		return eventGRPC2GoOBSSceneChange(q.ObsSceneChange), nil
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}

func eventGRPC2GoWindowFocusChange(
	q *streamd_grpc.EventWindowFocusChange,
) config.Event {
	return &event.WindowFocusChange{
		Host:        q.Host,
		WindowID:    q.WindowID,
		WindowTitle: q.WindowTitle,
		UserID:      q.UserID,
		ProcessID:   q.ProcessID,
		ProcessName: q.ProcessName,
		IsFocused:   q.IsFocused,
	}
}

func eventGRPC2GoOBSSceneChange(
	q *streamd_grpc.EventOBSSceneChange,
) config.Event {
	return &event.OBSSceneChange{
		NameFrom: q.From,
		NameTo:   q.To,
	}
}
