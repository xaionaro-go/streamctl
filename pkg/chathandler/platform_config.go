package chathandler

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	streamdconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

// FetchPlatformConfig retrieves the streamd config via gRPC and
// returns the AbstractPlatformConfig for the requested platform.
func FetchPlatformConfig(
	ctx context.Context,
	client streamd_grpc.StreamDClient,
	platName streamcontrol.PlatformName,
) (_ *streamcontrol.AbstractPlatformConfig, _err error) {
	logger.Tracef(ctx, "FetchPlatformConfig[%s]", platName)
	defer func() { logger.Tracef(ctx, "/FetchPlatformConfig[%s]: %v", platName, _err) }()

	reply, err := client.GetConfig(ctx, &streamd_grpc.GetConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("GetConfig from streamd: %w", err)
	}

	var cfg streamdconfig.Config
	if err := yaml.Unmarshal([]byte(reply.GetConfig()), &cfg); err != nil {
		return nil, fmt.Errorf("parse streamd config YAML: %w", err)
	}

	platCfg, ok := cfg.Backends[platName]
	if !ok {
		return nil, fmt.Errorf("platform %s not found in streamd config", platName)
	}

	return platCfg, nil
}
