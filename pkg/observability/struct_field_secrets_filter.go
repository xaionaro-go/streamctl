package observability

import (
	"reflect"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/object"
)

type StructFieldSecretsFilter struct{}

var _ logger.PreHook = (*StructFieldSecretsFilter)(nil)

var errorReflectValue = reflect.TypeOf((*error)(nil)).Elem()

func copyWithoutSecrets(in any) any {
	return object.DeepCopyWithoutSecrets(
		in,
		object.OptionWithVisitorFunc(
			func(
				ctx *object.ProcContext,
				v reflect.Value,
				sf *reflect.StructField,
			) (reflect.Value, bool, error) {
				t := v.Type()
				if t.Kind() == reflect.Interface {
					vElem := v.Elem()
					if !vElem.IsValid() {
						return v, false, nil
					}
					if vElem.Type().Implements(errorReflectValue) {
						return v, false, nil
					}
				}
				return v, true, nil
			},
		),
	)
}

func (StructFieldSecretsFilter) ProcessInput(
	_ belt.TraceIDs,
	_ logger.Level,
	args ...any,
) loggertypes.PreHookResult {
	for idx := range args {
		args[idx] = copyWithoutSecrets(args[idx])
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
		args[idx] = copyWithoutSecrets(args[idx])
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
		f.Value = copyWithoutSecrets(f.Value)
		return true
	})
	return loggertypes.PreHookResult{}
}
