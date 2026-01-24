package streamcontrol

import (
	"reflect"

	"github.com/goccy/go-yaml"
)

type StreamProfiles[S StreamProfile] map[ProfileName]S

func (profiles StreamProfiles[S]) Get(name ProfileName) (S, bool) {
	profile, ok := profiles[name]
	if !ok {
		return profile, false
	}

	return profiles.Resolve(profile), true
}

func (profiles StreamProfiles[S]) Resolve(profile S) S {
	var hierarchy []S
	hierarchy = append(hierarchy, profile)

	for {
		parentName, ok := profile.GetParent()
		if !ok {
			break
		}

		parentProfile, ok := profiles[parentName]
		if !ok {
			break
		}

		hierarchy = append(hierarchy, parentProfile)
		profile = parentProfile
	}

	result := hierarchy[len(hierarchy)-1]
	valueOfHierarchyItem := func(idx int) reflect.Value {
		item := hierarchy[idx]
		v := reflect.ValueOf(&item).Elem()
		if v.Kind() == reflect.Interface {
			v = v.Elem()
		}
		if v.Kind() == reflect.Pointer {
			v = v.Elem()
		}
		return v
	}

	v := valueOfHierarchyItem(len(hierarchy) - 1)
	if v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() == reflect.Struct {
		for i := 0; i < v.NumField(); i++ {
			fv := v.Field(i)
			if !fv.CanSet() {
				continue
			}
			for h := len(hierarchy) - 1; h >= 0; h-- {
				nv := valueOfHierarchyItem(h).Field(i)
				if isNil(nv) {
					continue
				}
				fv.Set(nv)
			}
		}
		return result
	}

	if _, ok := any(result).(RawMessage); ok {
		var merged map[string]any
		for h := len(hierarchy) - 1; h >= 0; h-- {
			item := any(hierarchy[h]).(RawMessage)
			var m map[string]any
			err := yaml.Unmarshal(item, &m)
			if err != nil {
				continue
			}
			if merged == nil {
				merged = m
				continue
			}
			for k, v := range m {
				if v == nil {
					continue
				}
				merged[k] = v
			}
		}
		b, err := yaml.Marshal(merged)
		if err != nil {
			return result
		}
		return any(RawMessage(b)).(S)
	}

	return result
}

func isNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Ptr, reflect.Slice, reflect.Map, reflect.Func, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}
