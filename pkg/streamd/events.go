package streamd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/expression"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/action"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config/event"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
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
	exprCtx := objToMap(ev)
	for _, rule := range d.Config.TriggerRules {
		if rule.EventQuery.Match(ev) {
			observability.Go(ctx, func() {
				err := d.doAction(ctx, rule.Action, exprCtx)
				if err != nil {
					logger.Errorf(ctx, "unable to perform action %#+v: %w", rule.Action, err)
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
) error {
	switch a := a.(type) {
	case *action.Noop:
		return nil
	case *action.StartStream:
		return d.StartStream(ctx, a.PlatID, a.Title, a.Description, a.Profile, a.CustomArgs...)
	case *action.EndStream:
		return d.EndStream(ctx, a.PlatID)
	case *action.OBSElementShowHide:
		value, err := expression.Eval[bool](a.ValueExpression, exprCtx)
		if err != nil {
			return fmt.Errorf("unable to Eval() the expression '%s': %w", a.ValueExpression, err)
		}
		return d.OBSElementSetShow(
			ctx,
			SceneElementIdentifier{
				Name: a.ElementName,
				UUID: a.ElementUUID,
			},
			value,
		)
	default:
		return fmt.Errorf("unknown action type: %T", a)
	}
}
