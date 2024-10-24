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
	type markerIsSecretT struct{}
	var markerIsSecret markerIsSecretT

	var secrets []string
	object.Traverse(obj, func(ctx *object.ProcContext, v reflect.Value, sf *reflect.StructField) (reflect.Value, bool, error) {
		if sf == nil {
			return v, true, nil
		}
		if !v.IsValid() {
			return v, false, nil
		}

		_, isSecret := sf.Tag.Lookup("secret")
		if !isSecret {
			isSecret = ctx.CustomData == markerIsSecret
		}
		if !isSecret {
			return v, true, nil
		}
		ctx.CustomData = markerIsSecret

		switch v.Kind() {
		case reflect.Struct:
			secrets = append(secrets, ParseStringsFrom(v.Interface())...)
			return v, false, nil
		case reflect.String:
			if v.String() == "" {
				return v, true, nil
			}
			secrets = append(secrets, v.String())
		}

		return v, true, nil
	})
	return secrets
}

func ParseStringsFrom(obj any) []string {
	var strings []string
	object.DeepCopy(
		obj,
		object.OptionWithProcessingFunc(func(ctx *object.ProcContext, v reflect.Value, sf *reflect.StructField) reflect.Value {
			switch v.Kind() {
			case reflect.String:
				if v.String() != "" {
					strings = append(strings, v.String())
				}
			}
			return v
		}),
		object.OptionWithUnexported(true),
	)
	return strings
}
