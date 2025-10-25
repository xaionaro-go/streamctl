package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/protobuf/go/streamcontrol_grpc"
)

func PlatformEventTypeGo2GRPC(
	ev streamcontrol.EventType,
) streamcontrol_grpc.PlatformEventType {
	switch ev {
	case streamcontrol.EventTypeUndefined:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeUndefined
	case streamcontrol.EventTypeChatMessage:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeChatMessage
	case streamcontrol.EventTypeCheer:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeCheer
	case streamcontrol.EventTypeAutoModHold:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeAutoModHold
	case streamcontrol.EventTypeAdBreak:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeAdBreak
	case streamcontrol.EventTypeBan:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeBan
	case streamcontrol.EventTypeFollow:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeFollow
	case streamcontrol.EventTypeRaid:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeRaid
	case streamcontrol.EventTypeChannelShoutoutReceive:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeChannelShoutoutReceive
	case streamcontrol.EventTypeSubscribe:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeSubscribe
	case streamcontrol.EventTypeStreamOnline:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeStreamOnline
	case streamcontrol.EventTypeStreamOffline:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeStreamOffline
	case streamcontrol.EventTypeOther:
		return streamcontrol_grpc.PlatformEventType_platformEventTypeOther
	}
	return streamcontrol_grpc.PlatformEventType_platformEventTypeOther
}

func PlatformEventTypeGRPC2Go(
	ev streamcontrol_grpc.PlatformEventType,
) streamcontrol.EventType {
	switch ev {
	case streamcontrol_grpc.PlatformEventType_platformEventTypeUndefined:
		return streamcontrol.EventTypeUndefined
	case streamcontrol_grpc.PlatformEventType_platformEventTypeChatMessage:
		return streamcontrol.EventTypeChatMessage
	case streamcontrol_grpc.PlatformEventType_platformEventTypeCheer:
		return streamcontrol.EventTypeCheer
	case streamcontrol_grpc.PlatformEventType_platformEventTypeAutoModHold:
		return streamcontrol.EventTypeAutoModHold
	case streamcontrol_grpc.PlatformEventType_platformEventTypeAdBreak:
		return streamcontrol.EventTypeAdBreak
	case streamcontrol_grpc.PlatformEventType_platformEventTypeBan:
		return streamcontrol.EventTypeBan
	case streamcontrol_grpc.PlatformEventType_platformEventTypeFollow:
		return streamcontrol.EventTypeFollow
	case streamcontrol_grpc.PlatformEventType_platformEventTypeRaid:
		return streamcontrol.EventTypeRaid
	case streamcontrol_grpc.PlatformEventType_platformEventTypeChannelShoutoutReceive:
		return streamcontrol.EventTypeChannelShoutoutReceive
	case streamcontrol_grpc.PlatformEventType_platformEventTypeSubscribe:
		return streamcontrol.EventTypeSubscribe
	case streamcontrol_grpc.PlatformEventType_platformEventTypeStreamOnline:
		return streamcontrol.EventTypeStreamOnline
	case streamcontrol_grpc.PlatformEventType_platformEventTypeStreamOffline:
		return streamcontrol.EventTypeStreamOffline
	case streamcontrol_grpc.PlatformEventType_platformEventTypeOther:
		return streamcontrol.EventTypeOther
	}
	return streamcontrol.EventTypeOther
}
