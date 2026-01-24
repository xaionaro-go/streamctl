package streamcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mockStreamController[ProfileType StreamProfile] struct {
	AccountCommons
	id          string
	applied     []ProfileType
	active      []bool
	titles      []string
	description []string
	ended       int
	shoutouts   []UserID
}

func (m *mockStreamController[ProfileType]) String() string {
	return m.id
}

func (m *mockStreamController[ProfileType]) GetStreams(ctx context.Context) ([]StreamInfo, error) {
	return nil, nil
}

func (m *mockStreamController[ProfileType]) ApplyProfile(ctx context.Context, streamID StreamID, profile ProfileType, customArgs ...any) error {
	m.applied = append(m.applied, profile)
	return nil
}

func (m *mockStreamController[ProfileType]) SetStreamActive(ctx context.Context, streamID StreamID, active bool) error {
	m.active = append(m.active, active)
	return nil
}

func (m *mockStreamController[ProfileType]) SetTitle(ctx context.Context, streamID StreamID, title string) error {
	m.titles = append(m.titles, title)
	return nil
}

func (m *mockStreamController[ProfileType]) SetDescription(ctx context.Context, streamID StreamID, description string) error {
	m.description = append(m.description, description)
	return nil
}

func (m *mockStreamController[ProfileType]) Flush(ctx context.Context, streamID StreamID) error {
	return nil
}

func (m *mockStreamController[ProfileType]) GetStreamStatus(ctx context.Context, streamID StreamID) (*StreamStatus, error) {
	return nil, nil
}

func (m *mockStreamController[ProfileType]) GetChatMessagesChan(ctx context.Context, streamID StreamID) (<-chan Event, error) {
	return nil, nil
}

func (m *mockStreamController[ProfileType]) SendChatMessage(ctx context.Context, streamID StreamID, message string) error {
	return nil
}

func (m *mockStreamController[ProfileType]) RemoveChatMessage(ctx context.Context, streamID StreamID, messageID EventID) error {
	return nil
}

func (m *mockStreamController[ProfileType]) BanUser(ctx context.Context, streamID StreamID, userID UserID, reason string, deadline time.Time) error {
	return nil
}

func (m *mockStreamController[ProfileType]) InsertAdsCuePoint(ctx context.Context, streamID StreamID, ts time.Time, duration time.Duration) error {
	return nil
}

func (m *mockStreamController[ProfileType]) Shoutout(ctx context.Context, streamID StreamID, chanID UserID) error {
	m.shoutouts = append(m.shoutouts, chanID)
	return nil
}

func (m *mockStreamController[ProfileType]) RaidTo(ctx context.Context, streamID StreamID, chanID UserID) error {
	return nil
}

func (m *mockStreamController[ProfileType]) IsCapable(ctx context.Context, cap Capability) bool {
	return true
}

func (m *mockStreamController[ProfileType]) IsChannelStreaming(ctx context.Context, chanID UserID) (bool, error) {
	return false, nil
}

func (m *mockStreamController[ProfileType]) Close() error {
	return nil
}

func TestStreamController(t *testing.T) {
	m1 := &mockStreamController[StreamProfile]{id: "m1"}
	ctx := context.Background()

	t.Run("SetTitle", func(t *testing.T) {
		err := m1.SetTitle(ctx, "stream1", "new title")
		require.NoError(t, err)
		require.Contains(t, m1.titles, "new title")
	})

	t.Run("Shoutout", func(t *testing.T) {
		err := m1.Shoutout(ctx, "stream1", UserID("user1"))
		require.NoError(t, err)
		require.Contains(t, m1.shoutouts, UserID("user1"))
	})
}

func TestRegisterPlatform(t *testing.T) {
	platID := PlatformID("mock-test")
	RegisterPlatform[mockAccountConfig, mockSProfile, struct{}](
		platID,
		0,
		func(ctx context.Context, cfg mockAccountConfig, saveCfgFn func(mockAccountConfig) error) (AccountGeneric[mockSProfile], error) {
			return &mockStreamController[mockSProfile]{id: "m1"}, nil
		},
	)

	cfg := Config{
		platID: &AbstractPlatformConfig{
			Accounts: map[AccountID]RawMessage{
				"test": RawMessage("{}"),
			},
		},
	}

	acc, err := NewAccount(context.Background(), platID, "test", cfg, nil)
	require.NoError(t, err)
	require.NotNil(t, acc)
	require.Equal(t, "m1", acc.String())
}

type mockAccountConfig struct {
	AccountConfigBase[mockSProfile]
}

func (m mockAccountConfig) IsInitialized() bool { return true }

type mockSProfile struct {
	StreamProfileBase
}

func TestYAMLMarshalStreamIDFullyQualified(t *testing.T) {
	id := NewStreamIDFullyQualified("plat", "acc", "stream")

	b, err := id.MarshalYAML()
	require.NoError(t, err)
	require.Equal(t, "plat:acc:stream", b)
}
