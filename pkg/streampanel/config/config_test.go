package config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

func TestConfigWithATagWriteRead(
	t *testing.T,
) {
	cfg := &Config{
		BuiltinStreamD: config.Config{
			Backends: streamcontrol.Config{
				youtube.ID: &streamcontrol.AbstractPlatformConfig{
					StreamProfiles: streamcontrol.StreamProfiles[streamcontrol.AbstractStreamProfile]{
						"profile": youtube.StreamProfile{
							Tags: []string{
								"tag",
							},
						},
					},
				},
			},
		},
	}

	var b bytes.Buffer
	_, err := cfg.WriteTo(&b)
	require.NoError(t, err)

	var cfgDup Config
	_, err = cfgDup.ReadFrom(&b)
	require.NoError(t, err)

	require.Equal(
		t,
		cfg.BuiltinStreamD.Backends[youtube.ID].StreamProfiles["profile"].(youtube.StreamProfile).Tags,
		cfgDup.BuiltinStreamD.Backends[youtube.ID].StreamProfiles["profile"].(youtube.StreamProfile).Tags,
	)
}
