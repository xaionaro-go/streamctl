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
				WindowFocusChange: triggerGo2GRPCWindowFocusChange(q),
			},
		}, nil
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}

func triggerGo2GRPCWindowFocusChange(q *event.WindowFocusChange) *streamd_grpc.EventWindowFocusChange {
	return &streamd_grpc.EventWindowFocusChange{
		WindowID:           q.WindowID,
		WindowTitle:        q.WindowTitle,
		WindowTitlePartial: q.WindowTitlePartial,
		UserID:             q.UserID,
	}
}

func EventGRPC2Go(in *streamd_grpc.Event) (config.Event, error) {
	switch q := in.EventOneOf.(type) {
	case *streamd_grpc.Event_WindowFocusChange:
		return triggerGRPC2GoWindowFocusChange(q.WindowFocusChange), nil
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}

func triggerGRPC2GoWindowFocusChange(
	q *streamd_grpc.EventWindowFocusChange,
) config.Event {
	return &event.WindowFocusChange{
		WindowID:           q.WindowID,
		WindowTitle:        q.WindowTitle,
		WindowTitlePartial: q.WindowTitlePartial,
		UserID:             q.UserID,
	}
}
