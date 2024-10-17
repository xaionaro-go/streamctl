package goconv

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

func TriggerRuleGo2GRPC(
	rule *api.TriggerRule,
) (*streamd_grpc.TriggerRule, error) {
	resultEventQuery, err := EventQueryGo2GRPC(
		rule.EventQuery,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the trigger query: %w", err)
	}

	resultAction, err := ActionGo2GRPC(rule.Action)
	if err != nil {
		return nil, fmt.Errorf("unable to convert the action: %w", err)
	}

	return &streamd_grpc.TriggerRule{
		Description: rule.Description,
		EventQuery:  resultEventQuery,
		Action:      resultAction,
	}, nil
}

func TriggerRuleGRPC2Go(
	rule *streamd_grpc.TriggerRule,
) (*api.TriggerRule, error) {
	resultEventQuery, err := EventQueryGRPC2Go(rule.GetEventQuery())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the trigger query: %w", err)
	}

	resultAction, err := ActionGRPC2Go(rule.GetAction())
	if err != nil {
		return nil, fmt.Errorf("unable to convert the action: %w", err)
	}

	return &api.TriggerRule{
		Description: rule.GetDescription(),
		EventQuery:  resultEventQuery,
		Action:      resultAction,
	}, nil
}
