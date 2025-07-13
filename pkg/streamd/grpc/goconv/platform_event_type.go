package goconv

import (
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func PlatformEventTypeGo2GRPC(
	ev streamcontrol.EventType,
) streamd_grpc.PlatformEventType {
	switch ev {
	case streamcontrol.EventTypeUndefined:
		return streamd_grpc.PlatformEventType_platformEventTypeUndefined
	case streamcontrol.EventTypeChatMessage:
		return streamd_grpc.PlatformEventType_platformEventTypeChatMessage
	case streamcontrol.EventTypeCheer:
		return streamd_grpc.PlatformEventType_platformEventTypeCheer
	case streamcontrol.EventTypeAutoModHold:
		return streamd_grpc.PlatformEventType_platformEventTypeAutoModHold
	case streamcontrol.EventTypeAdBreak:
		return streamd_grpc.PlatformEventType_platformEventTypeAdBreak
	case streamcontrol.EventTypeBan:
		return streamd_grpc.PlatformEventType_platformEventTypeBan
	case streamcontrol.EventTypeFollow:
		return streamd_grpc.PlatformEventType_platformEventTypeFollow
	case streamcontrol.EventTypeRaid:
		return streamd_grpc.PlatformEventType_platformEventTypeRaid
	case streamcontrol.EventTypeChannelShoutoutReceive:
		return streamd_grpc.PlatformEventType_platformEventTypeChannelShoutoutReceive
	case streamcontrol.EventTypeSubscribe:
		return streamd_grpc.PlatformEventType_platformEventTypeSubscribe
	case streamcontrol.EventTypeStreamOnline:
		return streamd_grpc.PlatformEventType_platformEventTypeStreamOnline
	case streamcontrol.EventTypeStreamOffline:
		return streamd_grpc.PlatformEventType_platformEventTypeStreamOffline
	case streamcontrol.EventTypeOther:
		return streamd_grpc.PlatformEventType_platformEventTypeOther
	}
	return streamd_grpc.PlatformEventType_platformEventTypeOther
}

func PlatformEventTypeGRPC2Go(
	ev streamd_grpc.PlatformEventType,
) streamcontrol.EventType {
	switch ev {
	case streamd_grpc.PlatformEventType_platformEventTypeUndefined:
		return streamcontrol.EventTypeUndefined
	case streamd_grpc.PlatformEventType_platformEventTypeChatMessage:
		return streamcontrol.EventTypeChatMessage
	case streamd_grpc.PlatformEventType_platformEventTypeCheer:
		return streamcontrol.EventTypeCheer
	case streamd_grpc.PlatformEventType_platformEventTypeAutoModHold:
		return streamcontrol.EventTypeAutoModHold
	case streamd_grpc.PlatformEventType_platformEventTypeAdBreak:
		return streamcontrol.EventTypeAdBreak
	case streamd_grpc.PlatformEventType_platformEventTypeBan:
		return streamcontrol.EventTypeBan
	case streamd_grpc.PlatformEventType_platformEventTypeFollow:
		return streamcontrol.EventTypeFollow
	case streamd_grpc.PlatformEventType_platformEventTypeRaid:
		return streamcontrol.EventTypeRaid
	case streamd_grpc.PlatformEventType_platformEventTypeChannelShoutoutReceive:
		return streamcontrol.EventTypeChannelShoutoutReceive
	case streamd_grpc.PlatformEventType_platformEventTypeSubscribe:
		return streamcontrol.EventTypeSubscribe
	case streamd_grpc.PlatformEventType_platformEventTypeStreamOnline:
		return streamcontrol.EventTypeStreamOnline
	case streamd_grpc.PlatformEventType_platformEventTypeStreamOffline:
		return streamcontrol.EventTypeStreamOffline
	case streamd_grpc.PlatformEventType_platformEventTypeOther:
		return streamcontrol.EventTypeOther
	}
	return streamcontrol.EventTypeOther
}
