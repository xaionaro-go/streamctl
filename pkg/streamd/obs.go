package streamd

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/andreykaipov/goobs"
	"github.com/chai2010/webp"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
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
					logger.Errorf(ctx, "unable to disconnect from OBS: %w", err)
				} else {
					logger.Tracef(ctx, "disconnected from OBS")
				}
			}, nil
		},
	)
	return proxy, func() {}, nil
}

func getOBSImageBytes(
	ctx context.Context,
	obsServer obs_grpc.OBSServer,
	el config.DashboardElementConfig,
	obsState *streamtypes.OBSState,
) ([]byte, time.Time, error) {
	img, nextUpdateAt, err := el.Source.GetImage(ctx, obsServer, el, obsState)
	if err != nil {
		return nil, time.Now().
				Add(time.Second),
			fmt.Errorf(
				"unable to get the image from the source: %w",
				err,
			)
	}

	for _, filter := range el.Filters {
		img = filter.Filter(ctx, img)
	}

	var out bytes.Buffer
	err = webp.Encode(&out, img, &webp.Options{
		Lossless: el.ImageLossless,
		Quality:  float32(el.ImageQuality),
		Exact:    false,
	})
	if err != nil {
		return nil, time.Now().Add(time.Second), fmt.Errorf("unable to encode the image: %w", err)
	}

	return out.Bytes(), nextUpdateAt, nil
}

func (d *StreamD) initImageTaker(ctx context.Context) error {
	for elName, el := range d.Config.Dashboard.Elements {
		if el.Source == nil {
			continue
		}
		if _, ok := el.Source.(*config.DashboardSourceDummy); ok {
			continue
		}
		{
			elName, el := elName, el
			_ = el
			observability.Go(ctx, func() {
				logger.Debugf(ctx, "taker of image '%s'", elName)
				defer logger.Debugf(ctx, "/taker of image '%s'", elName)

				obsServer, obsServerClose, err := d.OBS(ctx)
				if obsServerClose != nil {
					defer obsServerClose()
				}
				if err != nil {
					logger.Errorf(ctx, "unable to init connection with OBS: %w", err)
					return
				}

				for {
					var (
						imgBytes     []byte
						nextUpdateAt time.Time
						err          error
					)

					waitUntilNextIteration := func() bool {
						if nextUpdateAt.IsZero() {
							return false
						}
						select {
						case <-ctx.Done():
							return false
						case <-time.After(time.Until(nextUpdateAt)):
							return true
						}
					}

					imgBytes, nextUpdateAt, err = getOBSImageBytes(ctx, obsServer, el, &d.OBSState)
					if err != nil {
						logger.Errorf(ctx, "unable to get the image of '%s': %v", elName, err)
						if !waitUntilNextIteration() {
							return
						}
						continue
					}

					err = d.SetVariable(ctx, consts.VarKeyImage(consts.ImageID(elName)), imgBytes)
					if err != nil {
						logger.Errorf(ctx, "unable to save the image of '%s': %w", elName, err)
						if !waitUntilNextIteration() {
							return
						}
						continue
					}

					if !waitUntilNextIteration() {
						return
					}
				}
			})
		}
	}
	return nil
}

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
