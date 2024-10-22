package observability

import (
	"context"
	"reflect"
	"strings"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
	loggertypes "github.com/facebookincubator/go-belt/tool/logger/types"
	"github.com/xaionaro-go/deepcopy"
)

type SecretsProvider interface {
	SecretWords() []string
}

type ctxKeyT uint

const (
	ctxKeySecretsProvider = ctxKeyT(iota)
)

func WithSecretsProvider(ctx context.Context, secretsProvider SecretsProvider) context.Context {
	return context.WithValue(ctx, ctxKeySecretsProvider, secretsProvider)
}

func SecretsProviderFromCtx(ctx context.Context) SecretsProvider {
	v := ctx.Value(ctxKeySecretsProvider)
	if v, ok := v.(SecretsProvider); ok {
		return v
	}
	return nil
}

type SecretValuesFilter struct {
	SecretsProvider SecretsProvider
}

func NewSecretValuesFilter(sp SecretsProvider) *SecretValuesFilter {
	return &SecretValuesFilter{
		SecretsProvider: sp,
	}
}

var _ logger.PreHook = (*SecretValuesFilter)(nil)

func (sf *SecretValuesFilter) ProcessInput(
	_ belt.TraceIDs,
	_ logger.Level,
	args ...any,
) loggertypes.PreHookResult {
	for idx, arg := range args {
		args[idx] = filterSecretValues(sf, arg)
	}
	return loggertypes.PreHookResult{}
}

func (sf *SecretValuesFilter) ProcessInputf(
	_ belt.TraceIDs,
	_ logger.Level,
	format string,
	args ...any,
) loggertypes.PreHookResult {
	if censored := filterSecretValues(sf, format); censored != format {
		logger.Errorf(context.TODO(), "secrets are leaking through the logging format message: %s != ", format, censored)
	}
	for idx, arg := range args {
		args[idx] = filterSecretValues(sf, arg)
	}
	return loggertypes.PreHookResult{}
}
func (sf *SecretValuesFilter) ProcessInputFields(
	_ belt.TraceIDs,
	_ logger.Level,
	message string,
	fields field.AbstractFields,
) loggertypes.PreHookResult {
	if censored := filterSecretValues(sf, message); censored != message {
		logger.Errorf(context.TODO(), "secrets are leaking through the logging message: %s != %s", message, censored)
	}
	fields.ForEachField(func(f *field.Field) bool {
		f.Value = filterSecretValues(sf, f.Value)
		return true
	})
	return loggertypes.PreHookResult{}
}

func filterSecretValues[T any](
	sf *SecretValuesFilter,
	in T,
) T {
	return deepcopy.DeepCopyWithProcessing(in, sf.filterSecretValuesInLeaf)
}

func (sf *SecretValuesFilter) filterSecretValuesInLeaf(v reflect.Value, f *reflect.StructField) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		t := v.Type()
		result := reflect.New(t).Elem()
		result.Set(reflect.New(t.Elem()))                           // result = (*T)(nil)
		result.Elem().Set(sf.filterSecretValuesInLeaf(v.Elem(), f)) // *result = *v
		return result
	case reflect.String:
		return reflect.ValueOf(filterSecretValuesInString(sf, v.String())).Convert(v.Type())
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			return v
		}

		orig := string(v.Bytes())
		censored := filterSecretValuesInString(sf, orig)
		if orig == censored {
			return v
		}

		return reflect.ValueOf([]byte(censored)).Convert(v.Type())
	default:
		return v
	}
}

func filterSecretValuesInString(
	sf *SecretValuesFilter,
	s string,
) string {
	secretWords := sf.SecretsProvider.SecretWords()
	for _, secret := range secretWords {
		if len(secret) == 0 {
			continue
		}

		s = strings.ReplaceAll(s, secret, "<HIDDEN>")
	}
	return s
}
