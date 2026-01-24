package streamcontrol

import (
	"context"
)

type AccountGeneric[ProfileType StreamProfile] interface {
	AccountCommons

	ApplyProfile(
		ctx context.Context,
		streamID StreamID,
		profile ProfileType,
		customArgs ...any,
	) error
}
