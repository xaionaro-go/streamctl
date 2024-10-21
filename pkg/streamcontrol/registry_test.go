package streamcontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const mockPlatformID = "mock-platform"

type mockPlatformSpecificConfig struct {
	SomeWackyData []byte
}

func (mockPlatformSpecificConfig) IsInitialized() bool {
	return true
}

type mockStreamProfile struct {
	Hello string
}

func (mockStreamProfile) GetParent() (ProfileName, bool) {
	return "", false
}
func (mockStreamProfile) GetOrder() int {
	return 0
}

func TestRegistry(
	t *testing.T,
) {
	RegisterPlatform[mockPlatformSpecificConfig, mockStreamProfile](mockPlatformID)

	cfg := Config{
		mockPlatformID: &AbstractPlatformConfig{
			Enable:         ptr(true),
			Config:         RawMessage("{}"),
			StreamProfiles: map[ProfileName]AbstractStreamProfile{},
			Custom:         map[string]any{},
		},
	}

	require.True(t, IsInitialized(cfg, mockPlatformID))
}
