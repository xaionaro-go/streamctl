package streamd

import (
	"context"
	"crypto"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
)

func (d *StreamD) GetVariable(
	ctx context.Context,
	key consts.VarKey,
) (api.VariableValue, error) {
	v, ok := d.Variables.Load(key)
	if !ok {
		return nil, ErrNoVariable{}
	}

	b, ok := v.(api.VariableValue)
	if !ok {
		return nil, ErrVariableWrongType{}
	}

	return b, nil
}

func (d *StreamD) GetVariableHash(
	ctx context.Context,
	key consts.VarKey,
	hashType crypto.Hash,
) ([]byte, error) {
	b, err := d.GetVariable(ctx, key)
	if err != nil {
		return nil, err
	}

	hasher := hashType.New()
	hasher.Write(b)
	hash := hasher.Sum(nil)
	return hash, nil
}

func topicForVariable(key consts.VarKey) string {
	return fmt.Sprintf("var:%s", key)
}

func (d *StreamD) SetVariable(
	ctx context.Context,
	key consts.VarKey,
	value api.VariableValue,
) error {
	logger.Tracef(ctx, "SetVariable(ctx, '%s', value [len == %d])", key, len(value))
	defer logger.Tracef(ctx, "/SetVariable(ctx, '%s', value [len == %d])", key, len(value))
	d.Variables.Store(key, value)
	d.EventBus.Publish(topicForVariable(key), value)
	return nil
}

func (d *StreamD) SubscribeToVariable(
	ctx context.Context,
	varKey consts.VarKey,
) (<-chan api.VariableValue, error) {
	return eventSubToChanUsingTopic[api.VariableValue](ctx, d, nil, topicForVariable(varKey))
}
