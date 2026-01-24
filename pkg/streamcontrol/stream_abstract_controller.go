package streamcontrol

import (
	"context"
	"fmt"
)

type abstractStreamController[ProfileType StreamProfile] struct {
	StreamGeneric[ProfileType]
}

var _ AbstractStream = (*abstractStreamController[StreamProfile])(nil)

func (s *abstractStreamController[ProfileType]) GetImplementation() StreamCommon {
	return s.StreamGeneric
}

func (s *abstractStreamController[ProfileType]) ApplyProfile(
	ctx context.Context,
	profile StreamProfile,
	customArgs ...any,
) error {
	p, err := AssertStreamProfile[ProfileType](ctx, profile)
	if err != nil {
		return fmt.Errorf("unable to convert the profile: %w", err)
	}
	return s.StreamGeneric.ApplyProfile(ctx, *p, customArgs...)
}
