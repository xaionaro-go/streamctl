package observability

import (
	"context"
	"reflect"

	"github.com/xaionaro-go/object"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

type SecretsStaticProvider struct {
	xsync.Mutex
	SecretWordValues []string
}

var _ SecretsProvider = (*SecretsStaticProvider)(nil)

func NewStaticSecretsProvider() *SecretsStaticProvider {
	return &SecretsStaticProvider{}
}

func (sp *SecretsStaticProvider) SetSecretWords(v []string) {
	sp.Do(xsync.WithNoLogging(context.TODO(), true), func() {
		sp.SecretWordValues = v
	})
}

func (sp *SecretsStaticProvider) SecretWords() []string {
	return xsync.DoR1(xsync.WithNoLogging(context.TODO(), true), &sp.Mutex, func() []string {
		return sp.SecretWordValues
	})
}

func (sp *SecretsStaticProvider) ParseSecretsFrom(obj any) {
	secrets := ParseSecretsFrom(obj)
	sp.SetSecretWords(secrets)
}

func ParseSecretsFrom(obj any) []string {
	var secrets []string
	object.Traverse(obj, func(ctx *object.ProcContext, v reflect.Value, sf *reflect.StructField) (reflect.Value, bool, error) {
		if v.Kind() != reflect.Struct {
			return v, true, nil
		}
		encryptedField := v.FieldByName("encryptedMessage")
		if !encryptedField.IsValid() {
			return v, true, nil
		}
		getSecretFunc := v.MethodByName("Get")
		if !getSecretFunc.IsValid() {
			return v, true, nil
		}

		// is a secret...

		secretValue := getSecretFunc.Call([]reflect.Value{})[0]
		if !secretValue.IsValid() {
			return v, false, nil
		}
		secrets = append(secrets, ParseStringsFrom(secretValue.Interface())...)
		return v, false, nil
	})
	return secrets
}

func ParseStringsFrom(obj any) []string {
	var strings []string
	object.Traverse(
		obj,
		func(
			ctx *object.ProcContext,
			v reflect.Value,
			sf *reflect.StructField,
		) (reflect.Value, bool, error) {
			switch v.Kind() {
			case reflect.String:
				if v.String() != "" {
					strings = append(strings, v.String())
				}
			}
			return v, true, nil
		},
	)
	return strings
}
