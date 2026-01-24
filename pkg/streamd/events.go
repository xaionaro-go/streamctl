package streamd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/eventbus"
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
			observability.Go(ctx, func(ctx context.Context) {
				err := d.doAction(ctx, rule.Action, exprCtx)
				if err != nil {
					logger.Errorf(ctx, "unable to perform action %s: %v", rule.Action, err)
				}
			})
		}
	}
	return nil
}

func publishEvent[E any](
	ctx context.Context,
	bus *eventbus.EventBus,
	event E,
) {
	logger.Debugf(ctx, "publishEvent[%T](ctx, %#+v)", event, event)
	defer logger.Debugf(ctx, "/publishEvent[%T](ctx, %#+v)", event, event)
	result := eventbus.SendEvent(ctx, bus, event)
	if result.DropCountImmediate != 0 || result.DropCountDeferred != 0 {
		logger.Warnf(ctx, "unable to deliver the event to some of the subscriptions: %d + %d", result.DropCountImmediate, result.DropCountDeferred)
	}
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
	case *action.SetStreamActive:
		return d.SetStreamActive(ctx, a.StreamID, a.IsActive)
	case *action.SetTitle:
		return d.SetTitle(ctx, a.StreamID, a.Title)
	case *action.SetDescription:
		return d.SetDescription(ctx, a.StreamID, a.Description)
	case *action.ApplyProfile:
		cfg, err := d.GetConfig(ctx)
		if err != nil {
			return err
		}
		platCfg := cfg.Backends[a.StreamID.PlatformID]
		if platCfg == nil {
			return fmt.Errorf("platform %s not found in config", a.StreamID.PlatformID)
		}
		profileRaw, ok := platCfg.Accounts[a.StreamID.AccountID]
		if !ok {
			return fmt.Errorf("account %s not found for platform %s", a.StreamID.AccountID, a.StreamID.PlatformID)
		}
		profilesByStream := profileRaw.GetStreamProfiles()
		sProfs, ok := profilesByStream[a.StreamID.StreamID]
		if !ok {
			return fmt.Errorf("stream %s not found for account %s on platform %s", a.StreamID.StreamID, a.StreamID.AccountID, a.StreamID.PlatformID)
		}
		profile, ok := sProfs[a.Profile]
		if !ok {
			return fmt.Errorf("profile %s not found for stream %s of account %s on platform %s", a.Profile, a.StreamID.StreamID, a.StreamID.AccountID, a.StreamID.PlatformID)
		}
		return d.ApplyProfile(ctx, a.StreamID, profile)
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
	eventBus *eventbus.EventBus,
	queueSize uint,
	onReady func(ctx context.Context, outCh chan T),
) (<-chan T, error) {
	var topic T
	logger.Debugf(ctx, "eventSubToChan[%T]", topic)
	defer func() { logger.Debugf(ctx, "/eventSubToChan[%T]", topic) }()
	return eventSubToChanUsingTopic(ctx, eventBus, queueSize, onReady, topic)
}

func eventSubToChanUsingTopic[T, E any](
	ctx context.Context,
	eventBus *eventbus.EventBus,
	queueSize uint,
	onReady func(ctx context.Context, outCh chan E),
	topic T,
) (<-chan E, error) {
	var sample E
	logger.Debugf(ctx, "eventSubToChanUsingTopic[%T, %T]", topic, sample)
	defer func() { logger.Debugf(ctx, "/eventSubToChanUsingTopic[%T, %T]", topic, sample) }()

	opts := eventbus.Options{
		eventbus.OptionQueueSize(1),
		eventbus.OptionOnOverflow(eventbus.OnOverflowPileUpOrClose(queueSize, 10*time.Second)),
		eventbus.OptionOnUnsubscribe[T, E](func(_ context.Context, sub *eventbus.Subscription[T, E]) {
			logger.Debugf(ctx, "eventSubToChanUsingTopic[%T, %T]: unsubscribed", topic, sample)
		}),
	}
	if onReady != nil {
		opts = append(opts,
			eventbus.OptionOnSubscribed[T, E](func(
				ctx context.Context,
				sub *eventbus.Subscription[T, E],
			) {
				onReady(ctx, sub.EventChan())
			}),
		)
	}

	sub := eventbus.SubscribeWithCustomTopic[T, E](ctx, eventBus, topic, opts...)
	if sub == nil {
		return nil, fmt.Errorf("unable to subscribe to topic %v", topic)
	}
	return sub.EventChan(), nil
}

func (d *StreamD) SubscribeToDashboardChanges(
	ctx context.Context,
) (<-chan api.DiffDashboard, error) {
	return eventSubToChan[api.DiffDashboard](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToConfigChanges(
	ctx context.Context,
) (<-chan api.DiffConfig, error) {
	return eventSubToChan[api.DiffConfig](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamsChanges(
	ctx context.Context,
) (<-chan api.DiffStreams, error) {
	return eventSubToChan[api.DiffStreams](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamServersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamServers, error) {
	return eventSubToChan[api.DiffStreamServers](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamSinksChanges(
	ctx context.Context,
) (<-chan api.DiffStreamSinks, error) {
	return eventSubToChan[api.DiffStreamSinks](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamSourcesChanges(
	ctx context.Context,
) (<-chan api.DiffStreamSources, error) {
	return eventSubToChan[api.DiffStreamSources](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamForwardsChanges(
	ctx context.Context,
) (<-chan api.DiffStreamForwards, error) {
	return eventSubToChan[api.DiffStreamForwards](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) SubscribeToStreamPlayersChanges(
	ctx context.Context,
) (<-chan api.DiffStreamPlayers, error) {
	return eventSubToChan[api.DiffStreamPlayers](ctx, d.EventBus, 1000, nil)
}

func (d *StreamD) notifyStreamPlayerStart(
	ctx context.Context,
	streamSourceID streamtypes.StreamSourceID,
) {
	logger.Debugf(ctx, "notifyStreamPlayerStart")
	defer logger.Debugf(ctx, "/notifyStreamPlayerStart")

	publishEvent(ctx, d.EventBus, api.DiffStreamPlayers{})
}
