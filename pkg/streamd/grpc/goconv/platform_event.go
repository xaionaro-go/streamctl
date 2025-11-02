package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/goconv"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func ChatMessageGo2GRPC(
	event api.ChatMessage,
) streamd_grpc.ChatMessage {
	return streamd_grpc.ChatMessage{
		PlatID:  string(event.Platform),
		IsLive:  event.IsLive,
		Content: goconv.EventGo2GRPC(event.Event),
	}
}

func PlatformEventGRPC2Go(
	event *streamd_grpc.ChatMessage,
) api.ChatMessage {
	return api.ChatMessage{
		Platform: streamcontrol.PlatformName(event.GetPlatID()),
		IsLive:   event.GetIsLive(),
		Event:    goconv.EventGRPC2Go(event.GetContent()),
	}
}
