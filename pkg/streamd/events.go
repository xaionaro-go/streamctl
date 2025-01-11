package streamd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/expression"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) SubmitEvent(
	ctx context.Context,
	ev event.Event,
) error {
	return xsync.DoA2R1(ctx, &d.ConfigLock, d.submitEvent, ctx, ev)
}

func objToMap(obj any) map[string]any {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	m := map[string]any{}
	err = json.Unmarshal(b, &m)
	if err != nil {
		panic(err)
	}
	return m
}

func (d *StreamD) submitEvent(
	ctx context.Context,
	ev event.Event,
) error {
	logger.Tracef(ctx, "submitEvent(ctx, %s)", spew.Sdump(ev))
	defer logger.Tracef(ctx, "/submitEvent(ctx, %#v)", spew.Sdump(ev))
	exprCtx := objToMap(ev)
	for _, rule := range d.Config.TriggerRules {
		if rule.EventQuery.Match(ev) {
			observability.Go(ctx, func() {
				err := d.doAction(ctx, rule.Action, exprCtx)
				if err != nil {
					logger.Errorf(ctx, "unable to perform action %s: %v", rule.Action, err)
				}
			})
		}
	}
	return nil
}

func (d *StreamD) doAction(
	ctx context.Context,
	a action.Action,
	exprCtx any,
) (_err error) {
	logger.Debugf(ctx, "doAction: %s %#+v", a, exprCtx)
	defer func() { logger.Debugf(ctx, "/doAction: %s %#+v: %v", a, exprCtx, _err) }()
	switch a := a.(type) {
	case *action.Noop:
		return nil
	case *action.StartStream:
		return d.StartStream(ctx, a.PlatID, a.Title, a.Description, a.Profile, a.CustomArgs...)
	case *action.EndStream:
		return d.EndStream(ctx, a.PlatID)
	case *action.OBSItemShowHide:
		value, err := expression.Eval[bool](a.ValueExpression, exprCtx)
		if err != nil {
			return fmt.Errorf("unable to Eval() the expression '%s': %w", a.ValueExpression, err)
		}
		return d.OBSElementSetShow(
			ctx,
			SceneElementIdentifier{
				Name: a.ItemName,
				UUID: a.ItemUUID,
			},
			value,
		)
	default:
		return fmt.Errorf("unknown action type: %T", a)
	}
}

func eventSubToChan[T any](
	ctx context.Context,
	d *StreamD,
) (<-chan T, error) {
	var sample T
	topic := eventTopic(sample)

	var mutex sync.Mutex
	r := make(chan T)
	callback := func(in T) {
		mutex.Lock()
		defer mutex.Unlock()
		logger.Debugf(ctx, "eventSubToChan[%T]: received %#+v", sample, in)

		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case r <- in:
		case <-time.After(time.Minute):
			logger.Errorf(ctx, "unable to notify about '%s': timeout", topic)
		}
	}

	err := d.EventBus.SubscribeAsync(topic, callback, true)
	if err != nil {
		return nil, fmt.Errorf("unable to subscribe: %w", err)
	}

	observability.Go(ctx, func() {
		<-ctx.Done()

		mutex.Lock()
		defer mutex.Unlock()

		d.EventBus.Unsubscribe(topic, callback)
		d.EventBus.WaitAsync()
		close(r)
	})

	return r, nil
}

func (d *StreamD) SubscribeToDashboardChanges(
	ctx context.Context,
) (<-chan api.DiffDashboard, error) {
	return eventSubToChan[api.DiffDashboard](ctx, d)
}

func (d *StreamD) SubscribeToConfigChanges(
	ctx context.Context,
) (<-chan api.DiffConfig, error) {
	return eventSubToChan[api.DiffConfig](ctx, d)
}

func (d *StreamD) SubscribeToStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffStreams, error) {
	return eventSubToChan[api.DiffStreams](ctx, d)
}

func (d *StreamD) SubscribeToStreamServersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamServers, error) {
	return eventSubToChan[api.DiffStreamServers](ctx, d)
}

func (d *StreamD) SubscribeToStreamDestinationsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamDestinations, error) {
	return eventSubToChan[api.DiffStreamDestinations](ctx, d)
}

func (d *StreamD) SubscribeToIncomingStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffIncomingStreams, error) {
	return eventSubToChan[api.DiffIncomingStreams](ctx, d)
}

func (d *StreamD) SubscribeToStreamForwardsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamForwards, error) {
	return eventSubToChan[api.DiffStreamForwards](ctx, d)
}

func (d *StreamD) SubscribeToStreamPlayersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamPlayers, error) {
	return eventSubToChan[api.DiffStreamPlayers](ctx, d)
}

func (d *StreamD) notifyStreamPlayerStart(
	ctx context.Context,
	streamID streamtypes.StreamID,
) {
	logger.Debugf(ctx, "notifyStreamPlayerStart")
	defer logger.Debugf(ctx, "/notifyStreamPlayerStart")

	d.publishEvent(ctx, api.DiffStreamPlayers{})
}
