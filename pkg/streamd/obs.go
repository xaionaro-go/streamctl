package streamd

import (
	"context"
	"errors"
	"fmt"

	"github.com/andreykaipov/goobs"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) OBS(
	ctx context.Context,
) (obs_grpc.OBSServer, context.CancelFunc, error) {
	logger.Tracef(ctx, "OBS()")
	defer logger.Tracef(ctx, "/OBS()")

	proxy := obsgrpcproxy.New(
		ctx,
		func(ctx context.Context) (*goobs.Client, context.CancelFunc, error) {
			logger.Tracef(ctx, "OBS proxy getting client")
			defer logger.Tracef(ctx, "/OBS proxy getting client")
			obs := xsync.RDoR1(ctx, &d.ControllersLocker, func() *obs.OBS {
				return d.StreamControllers.OBS
			})
			if obs == nil {
				return nil, nil, fmt.Errorf("connection to OBS is not initialized")
			}

			client, err := obs.GetClient()
			logger.Tracef(ctx, "getting OBS client result: %v %v", client, err)
			if err != nil {
				return nil, nil, err
			}

			return client, func() {
				err := client.Disconnect()
				if err != nil {
					logger.Errorf(ctx, "unable to disconnect from OBS: %v", err)
				} else {
					logger.Tracef(ctx, "disconnected from OBS")
				}
			}, nil
		},
	)
	return proxy, func() {}, nil
}

var ErrNotChanged = errors.New("not changed")

type SceneElementIdentifier struct {
	Name *string
	UUID *string
}

func (d *StreamD) OBSElementSetShow(
	ctx context.Context,
	elID SceneElementIdentifier,
	shouldShow bool,
) error {
	if elID.Name == nil && elID.UUID == nil {
		return fmt.Errorf("elID.Name == nil && elID.UUID == nil (which is legit, but unexpected, so we fail just in case)")
	}

	obsServer, obsServerClose, err := d.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		return fmt.Errorf("unable to get a client to OBS: %w", err)
	}

	sceneListResp, err := obsServer.GetSceneList(ctx, &obs_grpc.GetSceneListRequest{})
	if err != nil {
		return fmt.Errorf("unable to get the scenes list: %w", err)
	}

	for _, scene := range sceneListResp.Scenes {
		itemList, err := obsServer.GetSceneItemList(ctx, &obs_grpc.GetSceneItemListRequest{
			SceneUUID: scene.SceneUUID,
		})
		if err != nil {
			return fmt.Errorf("unable to get the list of items of scene %#+v: %w", scene, err)
		}

		for _, item := range itemList.GetSceneItems() {
			if elID.Name != nil && *elID.Name != item.GetSourceName() {
				continue
			}
			if elID.UUID != nil && *elID.UUID != item.GetSourceUUID() {
				continue
			}

			req := &obs_grpc.SetSceneItemEnabledRequest{
				SceneName:        scene.SceneName,
				SceneUUID:        scene.SceneUUID,
				SceneItemID:      item.SceneItemID,
				SceneItemEnabled: shouldShow,
			}
			_, err := obsServer.SetSceneItemEnabled(ctx, req)
			if err != nil {
				return fmt.Errorf("unable to submit shouldShow:%v for %#+v: %w", shouldShow, item, err)
			}
		}
	}

	return nil
}
