package streamcontrol

import (
	"context"
	"fmt"
)

type abstractAccountController[ProfileType StreamProfile] struct {
	AccountGeneric[ProfileType]
}

var _ AbstractAccount = (*abstractAccountController[StreamProfile])(nil)

func (a *abstractAccountController[ProfileType]) GetImplementation() AccountCommons {
	return a.AccountGeneric
}

func (a *abstractAccountController[ProfileType]) ApplyProfile(
	ctx context.Context,
	streamID StreamID,
	profile StreamProfile,
	customArgs ...any,
) error {
	p, err := GetStreamProfile[ProfileType](ctx, profile)
	if err != nil {
		return fmt.Errorf("unable to convert the profile: %w", err)
	}
	return a.AccountGeneric.ApplyProfile(ctx, streamID, *p, customArgs...)
}
