package server

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	trequire "github.com/stretchr/testify/require"

	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	chatwebhook_grpc "github.com/xaionaro-go/chatwebhook/pkg/grpc/protobuf/go/chatwebhook_grpc"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	"github.com/xaionaro-go/streamctl/pkg/streamd/grpc/go/streamd_grpc"
)

// capturingStreamD is a minimal api.StreamD that records the listenerType
// argument passed by GRPCServer.InjectChatMessage. The interface is large
// (~100 methods); embedding the api.StreamD interface lets us implement
// only the method under test. Any other method call would dereference a
// nil interface and panic, which is the desired behaviour: the test must
// fail loudly if the handler ever calls something we haven't stubbed.
type capturingStreamD struct {
	api.StreamD

	calls []capturedInject
}

type capturedInject struct {
	platID       streamcontrol.PlatformName
	listenerType streamcontrol.ChatListenerType
	eventID      streamcontrol.EventID
}

func (c *capturingStreamD) InjectChatMessage(
	_ context.Context,
	platID streamcontrol.PlatformName,
	listenerType streamcontrol.ChatListenerType,
	ev streamcontrol.Event,
) error {
	c.calls = append(c.calls, capturedInject{
		platID:       platID,
		listenerType: listenerType,
		eventID:      ev.ID,
	})
	return nil
}

// TestGRPCInjectChatMessage_EmptyListenerType_DefaultsToPrimary covers the
// backward-compat coercion in GRPCServer.InjectChatMessage: a request from
// an old client with no listenerType field set must reach the inner StreamD
// as ChatListenerPrimary. Calls the handler directly (option b) — no
// network, no goroutine plumbing.
func TestGRPCInjectChatMessage_EmptyListenerType_DefaultsToPrimary(t *testing.T) {
	fake := &capturingStreamD{}
	srv := NewGRPCServer(fake)

	req := &streamd_grpc.InjectChatMessageRequest{
		PlatID:       "twitch",
		ListenerType: "", // explicit empty — the legacy-client wire shape
		Event:        &chatwebhook_grpc.Event{Id: "evt-1"},
	}

	_, err := srv.InjectChatMessage(context.Background(), req)
	trequire.NoError(t, err)

	trequire.Len(t, fake.calls, 1, "exactly one InjectChatMessage call must have reached StreamD")
	tassert.Equal(t, streamcontrol.ChatListenerPrimary, fake.calls[0].listenerType,
		"empty listenerType must coerce to ChatListenerPrimary so old clients keep working")
	tassert.Equal(t, streamcontrol.PlatformName("twitch"), fake.calls[0].platID)
}

// TestGRPCInjectChatMessage_ExplicitListenerType_PassedThrough is the
// dual-sided check for the test above: when the client DOES set
// listenerType, the canonical parser must convert it and the handler
// must NOT silently coerce to Primary. Without this, a typo / regression
// in the empty-string branch could let "alternate" requests slip through
// as primary.
func TestGRPCInjectChatMessage_ExplicitListenerType_PassedThrough(t *testing.T) {
	fake := &capturingStreamD{}
	srv := NewGRPCServer(fake)

	req := &streamd_grpc.InjectChatMessageRequest{
		PlatID:       "twitch",
		ListenerType: "alternate",
		Event:        &chatwebhook_grpc.Event{Id: "evt-1"},
	}

	_, err := srv.InjectChatMessage(context.Background(), req)
	trequire.NoError(t, err)

	trequire.Len(t, fake.calls, 1)
	tassert.Equal(t, streamcontrol.ChatListenerAlternate, fake.calls[0].listenerType,
		"non-empty listenerType must be parsed via the canonical stringer, not coerced")
}

// TestGRPCInjectChatMessage_InvalidListenerType_ReturnsError locks in the
// "client bug, not silent coercion" rule: an unrecognized listenerType
// string must surface as an error, NOT silently substitute Primary.
func TestGRPCInjectChatMessage_InvalidListenerType_ReturnsError(t *testing.T) {
	fake := &capturingStreamD{}
	srv := NewGRPCServer(fake)

	req := &streamd_grpc.InjectChatMessageRequest{
		PlatID:       "twitch",
		ListenerType: "garbage-not-a-type",
		Event:        &chatwebhook_grpc.Event{Id: "evt-1"},
	}

	_, err := srv.InjectChatMessage(context.Background(), req)
	trequire.Error(t, err, "invalid listenerType must surface as an error")
	tassert.Empty(t, fake.calls,
		"invalid input must NOT reach the inner StreamD — the handler rejects up-front")
}
