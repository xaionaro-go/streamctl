package observability

import (
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/object"
)

type StructFieldSecretsFilter struct{}

var _ logger.PreHook = (*StructFieldSecretsFilter)(nil)

func (StructFieldSecretsFilter) ProcessInput(
	_ belt.TraceIDs,
	_ logger.Level,
	args ...any,
) loggertypes.PreHookResult {
	for idx := range args {
		object.RemoveSecrets(&args[idx])
	}
	return loggertypes.PreHookResult{}
}

func (StructFieldSecretsFilter) ProcessInputf(
	_ belt.TraceIDs,
	_ logger.Level,
	format string,
	args ...any,
) loggertypes.PreHookResult {
	for idx := range args {
		object.RemoveSecrets(&args[idx])
	}
	return loggertypes.PreHookResult{}
}
func (StructFieldSecretsFilter) ProcessInputFields(
	_ belt.TraceIDs,
	_ logger.Level,
	message string,
	fields field.AbstractFields,
) loggertypes.PreHookResult {
	fields.ForEachField(func(f *field.Field) bool {
		object.RemoveSecrets(&f.Value)
		return true
	})
	return loggertypes.PreHookResult{}
}
