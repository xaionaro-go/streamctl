package goconv

import (
	"time"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
	"github.com/xaionaro-go/streamctl/pkg/xstring"
)

func ChatMessageGo2GRPC(
	event api.ChatMessage,
) streamd_grpc.ChatMessage {
	return streamd_grpc.ChatMessage{
		CreatedAtUNIXNano: uint64(event.CreatedAt.UnixNano()),
		PlatID:            string(event.Platform),
		IsLive:            event.IsLive,
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
) streamd_grpc.TextFormatType {
	switch formatType {
	case streamcontrol.TextFormatTypePlain:
		return streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN
	case streamcontrol.TextFormatTypeMarkdown:
		return streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_MARKDOWN
	case streamcontrol.TextFormatTypeHTML:
		return streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_HTML
	default:
		return streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_UNDEFINED
	}
}

func ChatMessageGRPC2Go(
	event *streamd_grpc.ChatMessage,
) api.ChatMessage {
	createdAtUNIXNano := event.GetCreatedAtUNIXNano()
	return api.ChatMessage{
		ChatMessage: streamcontrol.ChatMessage{
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
		},
		Platform: streamcontrol.PlatformName(event.GetPlatID()),
	}
}

func MessageFormatTypeGRPC2Go(
	formatType streamd_grpc.TextFormatType,
) streamcontrol.TextFormatType {
	switch formatType {
	case streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_PLAIN:
		return streamcontrol.TextFormatTypePlain
	case streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_MARKDOWN:
		return streamcontrol.TextFormatTypeMarkdown
	case streamd_grpc.TextFormatType_TEXT_FORMAT_TYPE_HTML:
		return streamcontrol.TextFormatTypeHTML
	default:
		return streamcontrol.TextFormatTypeUndefined
	}
}
