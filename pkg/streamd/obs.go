package streamd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andreykaipov/goobs"
	obsevents "github.com/andreykaipov/goobs/api/events"
	"github.com/andreykaipov/goobs/api/events/subscriptions"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/pkg/obsgrpcproxy"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/clock"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/obs"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) OBS(
	ctx context.Context,
	accountID streamcontrol.AccountID,
) (obs_grpc.OBSServer, context.CancelFunc, error) {
	logger.Tracef(ctx, "OBS()")
	defer logger.Tracef(ctx, "/OBS()")

	proxy := obsgrpcproxy.New(
		ctx,
		func(ctx context.Context) (*goobs.Client, context.CancelFunc, error) {
			logger.Tracef(ctx, "OBS proxy getting client")
			defer logger.Tracef(ctx, "/OBS proxy getting client")
			obsInstance := func() *obs.OBS {
				controllers := d.getControllersByPlatform(obs.ID)
				if controllers == nil {
					return nil
				}
				if accountID != "" {
					if c, ok := controllers[accountID]; ok {
						return c.GetImplementation().(*obs.OBS)
					}
					return nil
				}
				if c, ok := controllers[""]; ok {
					return c.GetImplementation().(*obs.OBS)
				}
				for _, c := range controllers {
					return c.GetImplementation().(*obs.OBS)
				}
				return nil
			}()
			if obsInstance == nil {
				return nil, nil, fmt.Errorf("connection to OBS is not initialized")
			}

			client, err := obsInstance.GetClient()
			logger.Tracef(ctx, "getting OBS client result: %v %v", client, err)
			if err != nil {
				return nil, nil, err
			}

			clientGoobs := client.GetGoobsClient()
			if clientGoobs == nil {
				return nil, nil, fmt.Errorf("the client is not a *goobs.Client (it is %T)", client)
			}

			return clientGoobs, func() {
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

func (d *StreamD) listenOBSEvents(
	ctx context.Context,
	o *obs.OBS,
) {
	logger.Debugf(ctx, "listenOBSEvents")
	defer logger.Debugf(ctx, "/listenOBSEvents")
	for {
		if o.IsClosed {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}

		client, err := o.GetClient(
			obs.GetClientOption(goobs.WithEventSubscriptions(
				0 |
					subscriptions.Scenes |
					subscriptions.InputVolumeMeters,
			)),
		)
		if err != nil {
			logger.Errorf(ctx, "unable to get an OBS client: %v", err)
			clock.Get().Sleep(time.Second)
			continue
		}

		func() {
			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-client.IncomingEvents():
					if !ok {
						return
					}
					d.processOBSEvent(ctx, ev)
				}
			}
		}()
	}
}

func (d *StreamD) processOBSEvent(
	ctx context.Context,
	ev any,
) {
	logger.Tracef(ctx, "got an OBS event: %T", ev)
	switch ev := ev.(type) {
	case *obsevents.CurrentProgramSceneChanged:
		d.onOBSSceneChanged(ctx, ev)
	case *obsevents.InputVolumeMeters:
		d.OBSState.Do(xsync.WithNoLogging(ctx, true), func() {
			for _, v := range ev.Inputs {
				d.OBSState.VolumeMeters[v.Name] = v.Levels
			}
		})
	}
}

func (d *StreamD) onOBSSceneChanged(
	ctx context.Context,
	obsEvent *obsevents.CurrentProgramSceneChanged,
) {
	cfg, err := d.GetConfig(ctx)
	if err != nil {
		logger.Errorf(ctx, "unable to get config: %v", err)
		return
	}

	event := &event.OBSSceneChange{
		NameTo: &obsEvent.SceneName,
		UUIDTo: &obsEvent.SceneUuid,
	}
	exprCtx := objToMap(event)

	for _, rule := range cfg.TriggerRules {
		if !rule.EventQuery.Match(event) {
			continue
		}
		observability.Go(ctx, func(ctx context.Context) {
			err := d.doAction(ctx, rule.Action, exprCtx)
			if err != nil {
				logger.Errorf(ctx, "unable to perform action %s: %v", rule.Action, err)
			}
		})
	}
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

	obsServer, obsServerClose, err := d.OBS(ctx, "")
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
