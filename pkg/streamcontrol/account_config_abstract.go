package streamcontrol

import (
	"context"
)

type AbstractAccountConfig = RawMessage

func ToAbstractAccountConfig[
	AC AccountConfigGeneric[SP],
	SP StreamProfile,
](
	ctx context.Context,
	cfg *AC,
) RawMessage {
	return ToRawMessage(*cfg)
}
