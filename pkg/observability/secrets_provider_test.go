package observability_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/secret"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
	"golang.org/x/oauth2"
)

func TestParseSecretsFrom(t *testing.T) {
	sample := config.Config{
		GitRepo: config.GitRepoConfig{
			PrivateKey: secret.New("1"),
		},
		Backends: map[streamcontrol.PlatformName]*streamcontrol.AbstractPlatformConfig{
			youtube.ID: {
				Config: &youtube.PlatformSpecificConfig{
					ChannelID:    "2",
					ClientID:     "3",
					ClientSecret: secret.New("4"),
					Token: ptr(secret.New(oauth2.Token{
						AccessToken:  "5",
						TokenType:    "6",
						RefreshToken: "7",
					})),
				},
			},
		},
	}
	secrets := observability.ParseSecretsFrom(sample)
	require.Equal(t, []string{"1", "4", "5", "6", "7"}, secrets)
}
