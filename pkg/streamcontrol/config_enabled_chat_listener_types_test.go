package streamcontrol

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unmarshalConfig is a test helper that unmarshals YAML into a Config
// and returns the EnabledChatListenerTypes for the given platform.
func unmarshalConfig(t *testing.T, yamlStr string, platName PlatformName) []ChatListenerType {
	t.Helper()
	cfg := Config{}
	err := yaml.Unmarshal([]byte(yamlStr), &cfg)
	require.NoError(t, err)
	platCfg, ok := cfg[platName]
	require.True(t, ok, "platform %q must exist in config", platName)
	return platCfg.EnabledChatListenerTypes
}

func TestConfig_EnabledChatListenerTypes_NilDefault(t *testing.T) {
	// When enabled_chat_listener_types is absent, the field should be nil,
	// meaning "use default" (NOT disabled).
	const input = `
testplat:
  enable: true
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	assert.Nil(t, types, "absent field must be nil (use default)")
	// Dual-sided: nil means NOT explicitly disabled — it's different from empty.
	assert.NotEqual(t, []ChatListenerType{}, types,
		"nil (use default) must not equal empty (disabled)")
}

func TestConfig_EnabledChatListenerTypes_EmptyDisables(t *testing.T) {
	// An explicit empty list [] means all listeners are disabled.
	const input = `
testplat:
  enable: true
  enabled_chat_listener_types: []
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types, "explicit [] must not be nil")
	assert.Empty(t, types, "explicit [] must be empty (disabled)")
	// Dual-sided: empty is NOT nil.
	assert.False(t, types == nil, "empty slice must not be nil")
}

func TestConfig_EnabledChatListenerTypes_ExplicitList(t *testing.T) {
	// When specific types are listed, exactly those types are enabled.
	const input = `
testplat:
  enable: true
  enabled_chat_listener_types:
    - primary
    - emergency
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types)
	require.Len(t, types, 2)
	assert.Equal(t, ChatListenerPrimary, types[0])
	assert.Equal(t, ChatListenerEmergency, types[1])
	// Dual-sided: list is NOT nil and NOT empty.
	assert.NotEmpty(t, types)
}

func TestConfig_Migration_DisableChatListenerTrue(t *testing.T) {
	// Old YAML with disable_chat_listener: true must migrate to
	// enabled_chat_listener_types: [] (all disabled).
	const input = `
testplat:
  enable: true
  disable_chat_listener: true
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types, "migrated value must not be nil")
	assert.Empty(t, types, "disable_chat_listener: true must migrate to empty list")
}

func TestConfig_Migration_DisableChatListenerFalse(t *testing.T) {
	// Old YAML with disable_chat_listener: false must migrate to
	// enabled_chat_listener_types: [primary].
	const input = `
testplat:
  enable: true
  disable_chat_listener: false
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types, "migrated value must not be nil")
	require.Len(t, types, 1)
	assert.Equal(t, ChatListenerPrimary, types[0])
}

func TestConfig_Migration_SkippedWhenNewFieldPresent(t *testing.T) {
	// When both old and new fields are present, the new field wins.
	const input = `
testplat:
  enable: true
  disable_chat_listener: true
  enabled_chat_listener_types:
    - alternate
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types)
	require.Len(t, types, 1)
	assert.Equal(t, ChatListenerAlternate, types[0],
		"new field must take precedence over old disable_chat_listener")
}

func TestConfig_YAMLRoundTrip_EmptyListPersists(t *testing.T) {
	// An explicit empty list in YAML must unmarshal as non-nil empty (disabled),
	// not degrade to nil (use default). This simulates the round-trip: the YAML
	// representation of an empty list is `[]`, and re-parsing it must preserve
	// the "disabled" semantic.
	const input = `
testplat:
  enable: true
  enabled_chat_listener_types: []
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.NotNil(t, types, "explicit [] must not become nil after unmarshal")
	assert.Empty(t, types, "explicit [] must remain empty")
	// Dual-sided: non-nil empty is NOT the same as nil (use default).
	assert.False(t, types == nil)
}

func TestConfig_YAMLRoundTrip_ExplicitListPersists(t *testing.T) {
	// Specific types in YAML must survive unmarshal with correct values and order.
	const input = `
testplat:
  enable: true
  enabled_chat_listener_types:
    - primary
    - contingency
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	require.Len(t, types, 2)
	assert.Equal(t, ChatListenerPrimary, types[0])
	assert.Equal(t, ChatListenerContingency, types[1])
}

func TestConfig_YAMLRoundTrip_AbsentFieldIsNil(t *testing.T) {
	// When the field is absent from YAML, it must unmarshal as nil (use default).
	const input = `
testplat:
  enable: true
  config: {}
`
	types := unmarshalConfig(t, input, "testplat")
	assert.Nil(t, types, "absent field must remain nil (use default)")
}
