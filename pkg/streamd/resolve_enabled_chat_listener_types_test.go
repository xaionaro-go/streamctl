package streamd

import (
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func TestResolveEnabledChatListenerTypes_NilConfig(t *testing.T) {
	result := resolveEnabledChatListenerTypes(nil)
	tassert.Nil(t, result, "nil config must return nil (no listeners)")
}

func TestResolveEnabledChatListenerTypes_NilEnabledTypes(t *testing.T) {
	cfg := &streamcontrol.AbstractPlatformConfig{
		EnabledChatListenerTypes: nil,
	}

	result := resolveEnabledChatListenerTypes(cfg)

	require.Len(t, result, 1, "nil EnabledChatListenerTypes must default to one entry")
	tassert.Equal(t, streamcontrol.ChatListenerPrimary, result[0],
		"default listener type must be Primary")
}

func TestResolveEnabledChatListenerTypes_EmptySlice(t *testing.T) {
	cfg := &streamcontrol.AbstractPlatformConfig{
		EnabledChatListenerTypes: []streamcontrol.ChatListenerType{},
	}

	result := resolveEnabledChatListenerTypes(cfg)

	require.NotNil(t, result, "empty slice must NOT be nil (it means explicitly disabled)")
	tassert.Empty(t, result, "empty slice must stay empty (all listeners disabled)")
}

func TestResolveEnabledChatListenerTypes_ExplicitTypes(t *testing.T) {
	explicit := []streamcontrol.ChatListenerType{
		streamcontrol.ChatListenerAlternate,
		streamcontrol.ChatListenerContingency,
	}
	cfg := &streamcontrol.AbstractPlatformConfig{
		EnabledChatListenerTypes: explicit,
	}

	result := resolveEnabledChatListenerTypes(cfg)

	tassert.Equal(t, explicit, result,
		"explicit types must be returned as-is")
}

// TestResolveEnabledChatListenerTypes_DualSided verifies the semantic
// contract: nil field means "default to Primary", empty slice means
// "disabled". These two cases must NOT be confused.
func TestResolveEnabledChatListenerTypes_DualSided(t *testing.T) {
	t.Run("default_IS_primary", func(t *testing.T) {
		cfg := &streamcontrol.AbstractPlatformConfig{
			EnabledChatListenerTypes: nil,
		}
		result := resolveEnabledChatListenerTypes(cfg)
		require.Len(t, result, 1)
		tassert.Equal(t, streamcontrol.ChatListenerPrimary, result[0],
			"nil field must resolve to [Primary]")
	})

	t.Run("empty_IS_disabled", func(t *testing.T) {
		cfg := &streamcontrol.AbstractPlatformConfig{
			EnabledChatListenerTypes: []streamcontrol.ChatListenerType{},
		}
		result := resolveEnabledChatListenerTypes(cfg)
		tassert.Empty(t, result, "empty slice must mean disabled")
		tassert.NotNil(t, result, "empty slice must not be nil")
	})

	t.Run("nil_and_empty_differ", func(t *testing.T) {
		nilCfg := &streamcontrol.AbstractPlatformConfig{
			EnabledChatListenerTypes: nil,
		}
		emptyCfg := &streamcontrol.AbstractPlatformConfig{
			EnabledChatListenerTypes: []streamcontrol.ChatListenerType{},
		}

		nilResult := resolveEnabledChatListenerTypes(nilCfg)
		emptyResult := resolveEnabledChatListenerTypes(emptyCfg)

		tassert.NotEqual(t, len(nilResult), len(emptyResult),
			"nil (default) and empty (disabled) must produce different results")
	})
}
