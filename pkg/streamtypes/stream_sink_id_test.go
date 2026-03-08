package streamtypes

import (
	"testing"

	goccyyaml "github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestStreamSinkIDFullyQualified_UnmarshalYAML_V2(t *testing.T) {
	tests := []struct {
		name     string
		yamlStr  string
		expected StreamSinkIDFullyQualified
	}{
		{
			name:    "full qualification",
			yamlStr: "custom:test/stream1/audio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeCustom,
				ID:   "test/stream1/audio",
			},
		},
		{
			name:    "default to custom",
			yamlStr: "test/stream1/audio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeCustom,
				ID:   "test/stream1/audio",
			},
		},
		{
			name:    "local type",
			yamlStr: "local:myaudio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeLocal,
				ID:   "myaudio",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual StreamSinkIDFullyQualified
			err := yaml.Unmarshal([]byte(tt.yamlStr), &actual)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestStreamSinkIDFullyQualified_UnmarshalYAML_Goccy(t *testing.T) {
	tests := []struct {
		name     string
		yamlStr  string
		expected StreamSinkIDFullyQualified
	}{
		{
			name:    "full qualification",
			yamlStr: "custom:test/stream1/audio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeCustom,
				ID:   "test/stream1/audio",
			},
		},
		{
			name:    "default to custom",
			yamlStr: "test/stream1/audio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeCustom,
				ID:   "test/stream1/audio",
			},
		},
		{
			name:    "local type",
			yamlStr: "local:myaudio",
			expected: StreamSinkIDFullyQualified{
				Type: StreamSinkTypeLocal,
				ID:   "myaudio",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var actual StreamSinkIDFullyQualified
			err := goccyyaml.Unmarshal([]byte(tt.yamlStr), &actual)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
