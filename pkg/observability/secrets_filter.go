package observability

import (
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/deepcopy"
)

type SecretsFilter struct{}

var _ logger.PreHook = (*SecretsFilter)(nil)

func (SecretsFilter) ProcessInput(
	_ belt.TraceIDs,
	_ logger.Level,
	args ...any,
) loggertypes.PreHookResult {
	for idx, arg := range args {
		args[idx] = deepcopy.DeepCopyWithoutSecrets(arg)
	}
	return loggertypes.PreHookResult{}
}

func (SecretsFilter) ProcessInputf(
	_ belt.TraceIDs,
	_ logger.Level,
	format string,
	args ...any,
) loggertypes.PreHookResult {
	for idx, arg := range args {
		args[idx] = deepcopy.DeepCopyWithoutSecrets(arg)
	}
	return loggertypes.PreHookResult{}
}
func (SecretsFilter) ProcessInputFields(
	_ belt.TraceIDs,
	_ logger.Level,
	message string,
	fields field.AbstractFields,
) loggertypes.PreHookResult {
	fields.ForEachField(func(f *field.Field) bool {
		f.Value = deepcopy.DeepCopyWithoutSecrets(f.Value)
		return true
	})
	return loggertypes.PreHookResult{}
}
