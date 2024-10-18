package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event/eventquery"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func EventQueryGo2GRPC(in eventquery.EventQuery) (*streamd_grpc.EventQuery, error) {
	switch q := in.(type) {
	case *eventquery.Event:
		ev, err := EventGo2GRPC(q.Event)
		if err != nil {
			return nil, fmt.Errorf("unable to convert event: %w", err)
		}
		return &streamd_grpc.EventQuery{
			EventQueryOneOf: &streamd_grpc.EventQuery_Event{
				Event: ev,
			},
		}, nil
	case *eventquery.EventType[*event.WindowFocusChange]:
		return &streamd_grpc.EventQuery{
			EventQueryOneOf: &streamd_grpc.EventQuery_EventType{
				EventType: streamd_grpc.EventType_eventWindowFocusChange,
			},
		}, nil
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}

func EventQueryGRPC2Go(in *streamd_grpc.EventQuery) (config.EventQuery, error) {
	switch q := in.GetEventQueryOneOf().(type) {
	case *streamd_grpc.EventQuery_Event:
		ev, err := EventGRPC2Go(q.Event)
		if err != nil {
			return nil, fmt.Errorf("unable to convert event: %w", err)
		}
		return &eventquery.Event{
			Event: ev,
		}, nil
	case *streamd_grpc.EventQuery_EventType:
		switch q.EventType {
		case streamd_grpc.EventType_eventWindowFocusChange:
			return &eventquery.EventType[*event.WindowFocusChange]{}, nil
		default:
			return nil, fmt.Errorf("unable to convert event type %v", q.EventType)
		}
	default:
		return nil, fmt.Errorf("conversion of type %T is not implemented, yet", q)
	}
}
