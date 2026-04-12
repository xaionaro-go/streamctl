package chathandler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

// stubFactory is a minimal ChatListenerFactory for testing the registry.
type stubFactory struct {
	platform streamcontrol.PlatformName
}

func (f *stubFactory) PlatformName() streamcontrol.PlatformName {
	return f.platform
}

func (f *stubFactory) SupportedChatListenerTypes() []streamcontrol.ChatListenerType {
	return []streamcontrol.ChatListenerType{streamcontrol.ChatListenerPrimary}
}

func (f *stubFactory) CreateChatListener(
	_ context.Context,
	_ *streamcontrol.AbstractPlatformConfig,
	_ streamcontrol.ChatListenerType,
) (ChatListener, error) {
	return nil, nil
}

func TestRegisterAndGetFactory(t *testing.T) {
	const platform = streamcontrol.PlatformName("test_register_and_get")
	f := &stubFactory{platform: platform}

	RegisterChatListenerFactory(f)

	got := GetChatListenerFactory(platform)
	require.NotNil(t, got, "registered factory must be retrievable")
	assert.Equal(t, platform, got.PlatformName())
	// Dual-sided: the exact factory instance is returned, not a copy.
	assert.Same(t, f, got)
}

func TestGetFactory_NotRegistered(t *testing.T) {
	got := GetChatListenerFactory("nonexistent_platform_xyz")
	assert.Nil(t, got, "unregistered platform must return nil")
}

func TestRegisterFactory_Duplicate_Overwrites(t *testing.T) {
	// The doc comment states: "Subsequent calls for the same platform
	// overwrite the previous factory." Verify that behavior.
	const platform = streamcontrol.PlatformName("test_dup_overwrite")
	first := &stubFactory{platform: platform}
	second := &stubFactory{platform: platform}

	RegisterChatListenerFactory(first)
	RegisterChatListenerFactory(second)

	got := GetChatListenerFactory(platform)
	require.NotNil(t, got)
	// Dual-sided: second factory IS returned, first IS NOT.
	assert.Same(t, second, got, "second registration must overwrite the first")
	assert.NotSame(t, first, got, "first factory must no longer be returned")
}
