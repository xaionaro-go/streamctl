package observability

import (
	"context"
	"fmt"
	"reflect"

	"github.com/xaionaro-go/deepcopy"
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
	deepcopy.DeepCopyWithProcessing(obj, func(in reflect.Value, sf *reflect.StructField) reflect.Value {
		if sf == nil {
			return in
		}
		if _, ok := sf.Tag.Lookup("secret"); !ok {
			return in
		}

		v := in
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}

		switch v.Kind() {
		case reflect.Struct:
			secrets = append(secrets, ParseStringsFrom(v.Interface())...)
		case reflect.String:
			if v.String() == "" {
				return in
			}
			secrets = append(secrets, v.String())
		default:
			panic(fmt.Errorf("support of filtering %v is not implemented", v.Kind()))
		}

		return in
	})
	return secrets
}

func ParseStringsFrom(obj any) []string {
	var strings []string
	deepcopy.DeepCopyWithProcessing(obj, func(in reflect.Value, sf *reflect.StructField) reflect.Value {
		v := in
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.String:
			if v.String() != "" {
				strings = append(strings, v.String())
			}
		}
		return in
	})
	return strings
}
