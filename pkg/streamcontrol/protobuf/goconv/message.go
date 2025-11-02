package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func MessageGo2GRPC(
	message streamcontrol.Message,
) *streamcontrol_grpc.Message {
	msg := &streamcontrol_grpc.Message{
		Content:    message.Content,
		FormatType: TextFormatTypeGo2GRPC(message.Format),
	}
	if message.InReplyTo != nil {
		msg.InReplyTo = ptr(string(*message.InReplyTo))
	}
	return msg
}

func MessageGRPC2Go(
	message *streamcontrol_grpc.Message,
) streamcontrol.Message {
	msg := streamcontrol.Message{
		Content: message.GetContent(),
		Format:  TextFormatTypeGRPC2Go(message.GetFormatType()),
	}
	if message.InReplyTo != nil {
		msg.InReplyTo = ptr(streamcontrol.EventID(*message.InReplyTo))
	}
	return msg
}
