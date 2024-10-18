package streampanel

import (
	"fmt"
	"reflect"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/serializable"
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
	l := logger.Default().WithField("name_prefix", namePrefix)

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
	case reflect.Bool:
		return []fyne.CanvasObject{newReflectField(v, func(i bool) {
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
	case reflect.Interface:
		typeNames := serializable.ListTypeNames[any]()
		var options []string
		values := map[string]any{}
		for _, n := range typeNames {
			candidate, ok := serializable.NewByTypeName[any](n)
			if !ok {
				continue
			}
			if reflect.ValueOf(candidate).Type().Implements(t) {
				options = append(options, n)
				values[n] = candidate
			}
		}
		l.Debugf("options == %v", options)

		container := container.NewVBox()
		var selector *widget.Select
		refreshContent := func() {
			container.RemoveAll()
			container.Add(selector)
			value := values[selector.Selected]
			fields := reflectMakeFieldsFor(
				reflect.ValueOf(value), reflect.TypeOf(value),
				setter,
				namePrefix,
			)
			l.Debugf("'%s' has %d fields", selector.Selected, len(fields))
			for _, field := range fields {
				container.Add(field)
			}
			setter(reflect.ValueOf(value))
		}
		selector = widget.NewSelect(options, func(s string) {
			refreshContent()
		})
		if len(options) > 0 {
			selector.SetSelected(options[0])
		}
		return []fyne.CanvasObject{container}
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
