package config

import (
	"bytes"
	"testing"

	"github.com/goccy/go-yaml"
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
					Accounts: map[streamcontrol.AccountID]streamcontrol.RawMessage{
						streamcontrol.DefaultAccountID: streamcontrol.ToRawMessage(streamcontrol.AccountConfigBase[streamcontrol.RawMessage]{
							StreamProfiles: map[streamcontrol.StreamID]streamcontrol.StreamProfiles[streamcontrol.RawMessage]{
								streamcontrol.DefaultStreamID: {
									"profile": streamcontrol.ToRawMessage(youtube.StreamProfile{
										Tags: []string{
											"tag",
										},
									}),
								},
							},
						}),
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

	getTags := func(c Config) []string {
		platCfg := c.BuiltinStreamD.Backends[youtube.ID]
		rawAccCfg := platCfg.Accounts[streamcontrol.DefaultAccountID]
		var accCfg streamcontrol.AccountConfigBase[streamcontrol.RawMessage]
		_ = yaml.Unmarshal(rawAccCfg, &accCfg)
		profiles := streamcontrol.GetStreamProfiles[youtube.StreamProfile](accCfg.StreamProfiles)
		return profiles[streamcontrol.DefaultStreamID]["profile"].Tags
	}

	require.Equal(
		t,
		getTags(*cfg),
		getTags(cfgDup),
	)
}
