package goconv

import (
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func EventGo2GRPC(
	event streamcontrol.Event,
) *streamcontrol_grpc.Event {
	ev := &streamcontrol_grpc.Event{
		Id:                string(event.ID),
		CreatedAtUNIXNano: uint64(event.CreatedAt.UnixNano()),
		EventType:         EventTypeGo2GRPC(event.Type),
		User:              UserGo2GRPC(event.User),
	}
	if event.ExpiresAt != nil {
		ev.ExpiresAtUNIXNano = ptr(uint64(event.ExpiresAt.UnixNano()))
	}
	if event.TargetUser != nil {
		ev.TargetUser = UserGo2GRPC(*event.TargetUser)
	}
	if event.TargetChannel != nil {
		ev.TargetChannel = UserGo2GRPC(*event.TargetChannel)
	}
	if event.Message != nil {
		ev.Message = MessageGo2GRPC(*event.Message)
	}
	if event.Paid != nil {
		ev.Money = MoneyGo2GRPC(*event.Paid)
	}
	if event.Tier != nil {
		ev.Tier = ptr(string(*event.Tier))
	}
	return ev
}

func TextFormatTypeGo2GRPC(
	formatType streamcontrol.TextFormatType,
) streamcontrol_grpc.TextFormatType {
	switch formatType {
	case streamcontrol.TextFormatTypePlain:
		return streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN
	case streamcontrol.TextFormatTypeMarkdown:
		return streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_MARKDOWN
	case streamcontrol.TextFormatTypeHTML:
		return streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_HTML
	default:
		return streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_UNDEFINED
	}
}

func EventGRPC2Go(
	event *streamcontrol_grpc.Event,
) streamcontrol.Event {
	ev := streamcontrol.Event{
		ID:        streamcontrol.EventID(event.Id),
		CreatedAt: time.Unix(0, int64(event.CreatedAtUNIXNano)),
		Type:      PlatformEventTypeGRPC2Go(event.EventType),
		User:      UserGRPC2Go(event.User),
	}
	if event.ExpiresAtUNIXNano != nil {
		ev.ExpiresAt = ptr(time.Unix(0, int64(*event.ExpiresAtUNIXNano)))
	}
	if event.TargetUser != nil {
		ev.TargetUser = ptr(UserGRPC2Go(event.TargetUser))
	}
	if event.TargetChannel != nil {
		ev.TargetChannel = ptr(UserGRPC2Go(event.TargetChannel))
	}
	if event.Message != nil {
		ev.Message = ptr(MessageGRPC2Go(event.Message))
	}
	if event.Money != nil {
		ev.Paid = ptr(MoneyGRPC2Go(event.Money))
	}
	if event.Tier != nil {
		ev.Tier = ptr(streamcontrol.Tier(*event.Tier))
	}
	return ev
}

func TextFormatTypeGRPC2Go(
	formatType streamcontrol_grpc.TextFormatType,
) streamcontrol.TextFormatType {
	switch formatType {
	case streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN:
		return streamcontrol.TextFormatTypePlain
	case streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_MARKDOWN:
		return streamcontrol.TextFormatTypeMarkdown
	case streamcontrol_grpc.TextFormatType_TEXT_FORMAT_TYPE_HTML:
		return streamcontrol.TextFormatTypeHTML
	default:
		return streamcontrol.TextFormatTypeUndefined
	}
}
