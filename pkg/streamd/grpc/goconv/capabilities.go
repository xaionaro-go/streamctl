package goconv

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func CapabilitiesGRPC2Go(
	ctx context.Context,
	caps []streamd_grpc.Capability,
) map[streamcontrol.Capability]struct{} {
	m := map[streamcontrol.Capability]struct{}{}

	for _, cap := range caps {
		var r streamcontrol.Capability
		switch cap {
		case streamd_grpc.Capability_SendChatMessage:
			r = streamcontrol.CapabilitySendChatMessage
		case streamd_grpc.Capability_DeleteChatMessage:
			r = streamcontrol.CapabilityDeleteChatMessage
		case streamd_grpc.Capability_BanUser:
			r = streamcontrol.CapabilityBanUser
		default:
			logger.Warnf(ctx, "unexpected capability: %v", cap)
			continue
		}
		m[r] = struct{}{}
	}

	return m
}

func CapabilitiesGo2GRPC(
	ctx context.Context,
	caps map[streamcontrol.Capability]struct{},
) []streamd_grpc.Capability {
	var result []streamd_grpc.Capability
	for cap := range caps {
		var item streamd_grpc.Capability
		switch cap {
		case streamcontrol.CapabilitySendChatMessage:
			item = streamd_grpc.Capability_SendChatMessage
		case streamcontrol.CapabilityDeleteChatMessage:
			item = streamd_grpc.Capability_DeleteChatMessage
		case streamcontrol.CapabilityBanUser:
			item = streamd_grpc.Capability_BanUser
		default:
			logger.Warnf(ctx, "unexpected capability: %v", cap)
			continue
		}
		result = append(result, item)
	}
	return result
}
