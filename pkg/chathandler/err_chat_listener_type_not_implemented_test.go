package chathandler

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

func TestErrChatListenerTypeNotImplemented_Error(t *testing.T) {
	err := ErrChatListenerTypeNotImplemented{
		PlatformName: "twitch",
		ListenerType: streamcontrol.ChatListenerPrimary,
	}
	msg := err.Error()
	assert.Contains(t, msg, "twitch", "error message must include the platform name")
	assert.Contains(t, msg, "primary", "error message must include the listener type")
}

func TestErrChatListenerTypeNotImplemented_ErrorsAs(t *testing.T) {
	original := ErrChatListenerTypeNotImplemented{
		PlatformName: "youtube",
		ListenerType: streamcontrol.ChatListenerAlternate,
	}
	// Wrap it to verify errors.As traverses the chain.
	wrapped := fmt.Errorf("factory failed: %w", original)

	var target ErrChatListenerTypeNotImplemented
	require.True(t, errors.As(wrapped, &target),
		"errors.As must match ErrChatListenerTypeNotImplemented")
	assert.Equal(t, streamcontrol.PlatformName("youtube"), target.PlatformName)
	assert.Equal(t, streamcontrol.ChatListenerAlternate, target.ListenerType)
}

func TestErrChatListenerTypeNotImplemented_NotOtherErrors(t *testing.T) {
	unrelated := fmt.Errorf("some other error")

	var target ErrChatListenerTypeNotImplemented
	// Dual-sided: unrelated errors do NOT match.
	assert.False(t, errors.As(unrelated, &target),
		"errors.As must not match unrelated error types")

	// Also verify errors.Is does not match either.
	actual := ErrChatListenerTypeNotImplemented{
		PlatformName: "kick",
		ListenerType: streamcontrol.ChatListenerContingency,
	}
	assert.False(t, errors.Is(unrelated, actual),
		"errors.Is must not match unrelated error against ErrChatListenerTypeNotImplemented")
}
