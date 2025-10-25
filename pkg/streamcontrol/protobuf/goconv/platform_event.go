package goconv

import (
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
	"github.com/xaionaro-go/streamctl/pkg/xstring"
)

func ChatMessageGo2GRPC(
	event streamcontrol.ChatMessage,
) streamcontrol_grpc.ChatMessage {
	return streamcontrol_grpc.ChatMessage{
		CreatedAtUNIXNano: uint64(event.CreatedAt.UnixNano()),
		EventType:         PlatformEventTypeGo2GRPC(event.EventType),
		UserID:            string(event.UserID),
		Username:          event.Username,
		UsernameReadable:  xstring.ToReadable(event.Username),
		MessageID:         string(event.MessageID),
		Message:           event.Message,
		MessageFormatType: MessageFormatTypeGo2GRPC(event.MessageFormatType),
	}
}

func MessageFormatTypeGo2GRPC(
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

func ChatMessageGRPC2Go(
	event *streamcontrol_grpc.ChatMessage,
) streamcontrol.ChatMessage {
	createdAtUNIXNano := event.GetCreatedAtUNIXNano()
	return streamcontrol.ChatMessage{
		CreatedAt: time.Unix(
			int64(createdAtUNIXNano)/int64(time.Second),
			(int64(createdAtUNIXNano)%int64(time.Second))/int64(time.Nanosecond),
		),
		EventType:         PlatformEventTypeGRPC2Go(event.GetEventType()),
		UserID:            streamcontrol.ChatUserID(event.GetUserID()),
		Username:          event.GetUsername(),
		MessageID:         streamcontrol.ChatMessageID(event.GetMessageID()),
		Message:           event.GetMessage(),
		MessageFormatType: MessageFormatTypeGRPC2Go(event.GetMessageFormatType()),
	}
}

func MessageFormatTypeGRPC2Go(
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
