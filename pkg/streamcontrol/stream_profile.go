package streamcontrol

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
)

type StreamProfile interface {
	GetParent() (ProfileName, bool)
	GetOrder() int
	GetTitle() (string, bool)
	GetDescription() (string, bool)
}

func AssertStreamProfile[T StreamProfile](
	ctx context.Context,
	v StreamProfile,
) (*T, error) {
	profile, ok := v.(T)
	if ok {
		return &profile, nil
	}

	profilePtr, ok := (any)(v).(*T)
	if ok {
		return profilePtr, nil
	}

	var zeroProfile T
	return nil, ErrInvalidStreamProfileType{Expected: zeroProfile, Received: v}
}

func GetStreamProfile[T StreamProfile](
	ctx context.Context,
	v StreamProfile,
) (*T, error) {
	logger.Debugf(ctx, "GetStreamProfile[%T](%T)", *new(T), v)
	profilePtr, err := AssertStreamProfile[T](ctx, v)
	if err == nil {
		return profilePtr, nil
	}
	logger.Debugf(ctx, "GetStreamProfile: AssertStreamProfile failed: %v", err)

	b, ok := v.(RawMessage)
	if !ok {
		var err error
		b, err = yaml.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("unable to serialize: %w: %#+v", err, v)
		}
	}
	logger.Debugf(ctx, "YAML representation: <%s>", b)
	var profile T
	err = yaml.Unmarshal(b, &profile)
	if err != nil {
		return nil, fmt.Errorf("unable to deserialize: %w: <%s>", err, b)
	}
	logger.Tracef(ctx, "converted %#+v (%s) to %#+v", v, string(b), profile)
	return &profile, nil
}
