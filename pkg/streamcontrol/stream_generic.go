package streamcontrol

import (
	"context"
)

type StreamGeneric[ProfileType StreamProfile] interface {
	StreamCommon

	ApplyProfile(
		ctx context.Context,
		profile ProfileType,
		customArgs ...any,
	) error
}
