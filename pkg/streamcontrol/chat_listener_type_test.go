package streamcontrol

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChatListenerType_String(t *testing.T) {
	tests := []struct {
		typ      ChatListenerType
		expected string
	}{
		{ChatListenerPrimary, "primary"},
		{ChatListenerAlternate, "alternate"},
		{ChatListenerContingency, "contingency"},
		{ChatListenerEmergency, "emergency"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.typ.String())
		})
	}

	t.Run("unknown", func(t *testing.T) {
		unknown := ChatListenerType(999)
		result := unknown.String()
		assert.Equal(t, "unknown_999", result)
		// Dual-sided: known types do NOT produce "unknown_" prefix
		for i := range EndOfChatListenerType {
			assert.NotContains(t, i.String(), "unknown_",
				"valid type %d should not have unknown_ prefix", i)
		}
	})
}

func TestChatListenerTypeFromString(t *testing.T) {
	t.Run("valid_strings", func(t *testing.T) {
		validCases := map[string]ChatListenerType{
			"primary":     ChatListenerPrimary,
			"alternate":   ChatListenerAlternate,
			"contingency": ChatListenerContingency,
			"emergency":   ChatListenerEmergency,
		}
		for s, expected := range validCases {
			got, err := ChatListenerTypeFromString(s)
			require.NoError(t, err, "parsing %q", s)
			assert.Equal(t, expected, got, "parsing %q", s)
		}
	})

	t.Run("invalid_strings", func(t *testing.T) {
		invalids := []string{"", "unknown", "PRIMARY", "Primary", "foo", "unknown_0"}
		for _, s := range invalids {
			_, err := ChatListenerTypeFromString(s)
			assert.Error(t, err, "parsing %q should fail", s)
		}
	})

	// Dual-sided: every valid type round-trips through String/FromString
	t.Run("all_valid_types_round_trip", func(t *testing.T) {
		for i := range EndOfChatListenerType {
			s := i.String()
			got, err := ChatListenerTypeFromString(s)
			require.NoError(t, err)
			assert.Equal(t, i, got)
		}
	})
}

func TestChatListenerType_MarshalJSON(t *testing.T) {
	for i := range EndOfChatListenerType {
		t.Run(i.String(), func(t *testing.T) {
			data, err := json.Marshal(i)
			require.NoError(t, err)

			// Should be a JSON string, not a number
			assert.Equal(t, fmt.Sprintf(`"%s"`, i.String()), string(data))

			var got ChatListenerType
			err = json.Unmarshal(data, &got)
			require.NoError(t, err)
			assert.Equal(t, i, got, "JSON round-trip must preserve type")
		})
	}
}

func TestChatListenerType_UnmarshalJSON(t *testing.T) {
	t.Run("invalid_value", func(t *testing.T) {
		var got ChatListenerType
		err := json.Unmarshal([]byte(`"bogus"`), &got)
		assert.Error(t, err)
	})

	t.Run("invalid_json", func(t *testing.T) {
		var got ChatListenerType
		err := json.Unmarshal([]byte(`123`), &got)
		assert.Error(t, err, "numeric JSON should not unmarshal as ChatListenerType")
	})
}

func TestChatListenerType_MarshalYAML(t *testing.T) {
	for i := range EndOfChatListenerType {
		t.Run(i.String(), func(t *testing.T) {
			data, err := yaml.Marshal(i)
			require.NoError(t, err)
			// YAML output is the string value followed by a newline
			assert.Equal(t, i.String()+"\n", string(data))

			var got ChatListenerType
			err = yaml.Unmarshal(data, &got)
			require.NoError(t, err)
			assert.Equal(t, i, got, "YAML round-trip must preserve type")
		})
	}
}

func TestChatListenerType_UnmarshalYAML(t *testing.T) {
	t.Run("invalid_value", func(t *testing.T) {
		var got ChatListenerType
		err := yaml.Unmarshal([]byte(`bogus`), &got)
		assert.Error(t, err)
	})
}
