package streamcontrol

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestStreamProfilesResolve(t *testing.T) {
	profiles := StreamProfiles[RawMessage]{
		"HQ": ToRawMessage(map[string]any{"title": "High Quality"}),
		"LQ": ToRawMessage(map[string]any{"title": "Low Quality"}),
	}

	// Test resolving an anonymous profile with a parent
	anonProfile := ToRawMessage(map[string]any{"parent": "HQ", "description": "some description"})
	resolved := profiles.Resolve(anonProfile)

	var res map[string]any
	err := yaml.Unmarshal(resolved, &res)
	require.NoError(t, err)
	require.Equal(t, "High Quality", res["title"])
	require.Equal(t, "some description", res["description"])
}

func TestStreamProfilesResolveDeep(t *testing.T) {
	profiles := StreamProfiles[RawMessage]{
		"Base": ToRawMessage(map[string]any{"title": "Base Title", "description": "Base Desc"}),
		"HQ":   ToRawMessage(map[string]any{"parent": "Base", "title": "High Quality"}),
	}

	anonProfile := ToRawMessage(map[string]any{"parent": "HQ", "order": 10})
	resolved := profiles.Resolve(anonProfile)

	var res map[string]any
	err := yaml.Unmarshal(resolved, &res)
	require.NoError(t, err)
	require.Equal(t, "High Quality", res["title"])
	require.Equal(t, "Base Desc", res["description"])
	require.Equal(t, "10", fmt.Sprintf("%v", res["order"]))
}
