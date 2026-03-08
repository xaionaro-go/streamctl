package config

import (
	"encoding/json"
	"fmt"
)

func toMap[T any](in T) map[string]any {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	m := map[string]any{}
	err = json.Unmarshal(b, &m)
	if err != nil {
		panic(err)
	}
	return m
}

func fromMap(m map[string]any, result any) {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(b, &result)
	if err != nil {
		panic(fmt.Errorf("unable to un-JSON-ize '%s': %w", b, err))
	}
}
