package streamd

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/xsync"
)

func (d *StreamD) AddTriggerRule(
	ctx context.Context,
	triggerRule *api.TriggerRule,
) (api.TriggerRuleID, error) {
	logger.Debugf(ctx, "AddTriggerRule(ctx, %s)", triggerRule)
	defer logger.Debugf(ctx, "/AddTriggerRule(ctx, %s)", triggerRule)
	ruleID, err := d.addTriggerRuleToConfig(ctx, triggerRule)
	if err != nil {
		return 0, fmt.Errorf("unable to add the trigger rule to config: %w", err)
	}
	return ruleID, nil
}

func (d *StreamD) addTriggerRuleToConfig(
	ctx context.Context,
	triggerRule *api.TriggerRule,
) (api.TriggerRuleID, error) {
	return xsync.DoR2(ctx, &d.ConfigLock, func() (api.TriggerRuleID, error) {
		ruleID := api.TriggerRuleID(len(d.Config.TriggerRules))
		d.Config.TriggerRules = append(d.Config.TriggerRules, triggerRule)
		if err := d.SaveConfig(ctx); err != nil {
			return ruleID, fmt.Errorf("unable to save the config: %w", err)
		}
		return ruleID, nil
	})
}

func (d *StreamD) UpdateTriggerRule(
	ctx context.Context,
	ruleID api.TriggerRuleID,
	triggerRule *api.TriggerRule,
) error {
	logger.Debugf(ctx, "UpdateTriggerRule(ctx, %v, %s)", ruleID, triggerRule)
	defer logger.Debugf(ctx, "/UpdateTriggerRule(ctx, %v, %s)", ruleID, triggerRule)
	if err := d.updateTriggerRuleInConfig(ctx, ruleID, triggerRule); err != nil {
		return fmt.Errorf("unable to update the trigger rule %d in the config: %w", ruleID, err)
	}
	return nil
}

func (d *StreamD) updateTriggerRuleInConfig(
	ctx context.Context,
	ruleID api.TriggerRuleID,
	triggerRule *api.TriggerRule,
) error {
	return xsync.DoR1(ctx, &d.ConfigLock, func() error {
		if ruleID >= api.TriggerRuleID(len(d.Config.TriggerRules)) {
			return fmt.Errorf("rule %d does not exist", ruleID)
		}
		d.Config.TriggerRules[ruleID] = triggerRule
		if err := d.SaveConfig(ctx); err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}
		return nil
	})
}

func (d *StreamD) RemoveTriggerRule(
	ctx context.Context,
	ruleID api.TriggerRuleID,
) error {
	logger.Debugf(ctx, "RemoveTriggerRule(ctx, %v)", ruleID)
	defer logger.Debugf(ctx, "/RemoveTriggerRule(ctx, %v)", ruleID)
	if err := d.removeTriggerRuleFromConfig(ctx, ruleID); err != nil {
		return fmt.Errorf("unable to remove the trigger rule %d from the config: %w", ruleID, err)
	}
	return nil
}

func (d *StreamD) removeTriggerRuleFromConfig(
	ctx context.Context,
	ruleID api.TriggerRuleID,
) error {
	return xsync.DoR1(ctx, &d.ConfigLock, func() error {
		if ruleID >= api.TriggerRuleID(len(d.Config.TriggerRules)) {
			return fmt.Errorf("rule %d does not exist", ruleID)
		}
		d.Config.TriggerRules = append(d.Config.TriggerRules[:ruleID], d.Config.TriggerRules[ruleID+1:]...)
		if err := d.SaveConfig(ctx); err != nil {
			return fmt.Errorf("unable to save the config: %w", err)
		}
		return nil
	})
}

func (d *StreamD) ListTriggerRules(
	ctx context.Context,
) (api.TriggerRules, error) {
	logger.Debugf(ctx, "ListTriggerRules(ctx)")
	defer logger.Debugf(ctx, "/ListTriggerRules(ctx)")
	return xsync.DoR2(ctx, &d.ConfigLock, func() (api.TriggerRules, error) {
		rules := make(api.TriggerRules, len(d.Config.TriggerRules))
		copy(rules, d.Config.TriggerRules)
		return rules, nil
	})
}
