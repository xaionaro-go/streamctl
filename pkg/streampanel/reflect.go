package streampanel

import (
	"fmt"
	"reflect"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func makeFieldsFor(
	obj any,
) []fyne.CanvasObject {
	return reflectMakeFieldsFor(reflect.ValueOf(obj), reflect.ValueOf(obj).Type(), nil, "")
}

func isUIDisabled(
	obj any,
) bool {
	return reflectIsUIDisabled(reflect.ValueOf(obj))
}

func reflectIsUIDisabled(
	v reflect.Value,
) bool {
	v = reflect.Indirect(v)
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).Name == "uiDisable" {
			return true
		}
	}
	return false
}

func reflectMakeFieldsFor(
	v reflect.Value,
	t reflect.Type,
	setter func(reflect.Value),
	namePrefix string,
) []fyne.CanvasObject {
	if setter == nil {
		setter = func(v reflect.Value) {}
	}

	switch t.Kind() {
	case reflect.String:
		return []fyne.CanvasObject{newReflectField(v, func(s string) {
			setter(reflect.ValueOf(s).Convert(t))
		}, namePrefix)}
	case reflect.Uint64:
		return []fyne.CanvasObject{newReflectField(v, func(i uint64) {
			setter(reflect.ValueOf(i).Convert(t))
		}, namePrefix)}
	case reflect.Ptr:
		return reflectMakeFieldsFor(v.Elem(), t.Elem(), func(newValue reflect.Value) {
			if newValue.IsZero() && v.CanAddr() {
				v.Set(reflect.Zero(t))
				setter(v)
				return
			}

			if !v.Elem().IsValid() {
				v.Set(reflect.New(t.Elem()))
			}
			v.Elem().Set(newValue)
			setter(v)
		}, namePrefix)
	case reflect.Struct:
		var result []fyne.CanvasObject
		for i := 0; i < v.NumField(); i++ {
			fv := v.Field(i)
			ft := t.Field(i)
			if ft.PkgPath != "" {
				if ft.Name == "uiDisable" {
					return nil
				}
				tag := ft.Tag.Get("uicomment")
				if tag != "" {
					result = append(result, widget.NewLabel(tag))
				}
				continue // a non-exported field
			}

			result = append(result, reflectMakeFieldsFor(fv, ft.Type, func(newValue reflect.Value) {
				fv.Set(newValue)
				setter(v)
			}, namePrefix+"."+ft.Name)...)
		}
		return result
	default:
		panic(fmt.Errorf("internal error: %s: support of %v is not implemented, yet", namePrefix, t.Kind()))
	}
}

func newReflectField[T any](
	curValue reflect.Value,
	setter func(T),
	fieldName string,
) fyne.CanvasObject {
	result := widget.NewEntry()
	if curValue.IsValid() {
		result.SetText(fmt.Sprintf("%v", curValue.Interface()))
	}
	result.SetPlaceHolder(fieldName)
	result.OnChanged = func(s string) {
		setter(from[T](s))
	}
	return result
}

func from[T any](in string) T {
	var result T
	if in == "" {
		return result
	}
	fmt.Sscanf(in, "%v", &result)
	return result
}
