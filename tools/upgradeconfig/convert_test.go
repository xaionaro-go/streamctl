package upgradeconfig

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestConvertConfig(t *testing.T) {
	oldYAML := `cache_path: "~/.streamd.cache"
chat_messages_storage: "~/.streamd.chat"
git_repo:
  enable: true
  url: "git@github.com:user/repo.git"
backends:
  twitch:
    enable: true
    config:
      channel: "channel1"
      client_id: "clientid1"
    stream_profiles:
      profile1:
        parent: "parent1"
        order: 10
        tags: ["tag1", "tag2"]
      profile2:
        order: 20
    custom:
      key1: "value1"
      key2: 42
  obs:
    enable: true
    config:
      host: "localhost"
    stream_profiles:
      obs_p1:
        order: 5
      obs_p2: {}
profile_metadata:
  profile1:
    default_stream_title: "Title 1"
  profile2:
    default_stream_title: "Title 2"
monitor:
  elements:
    elem1:
      width: 100
    elem2:
      width: 200
`

	converted, err := ConvertConfigBytes([]byte(oldYAML))
	require.NoError(t, err)
	t.Logf("Converted YAML:\n%s", string(converted))

	var cfg map[string]any
	err = yaml.Unmarshal(converted, &cfg)
	require.NoError(t, err)

	// Check top-level fields
	require.Equal(t, "~/.streamd.cache", cfg["cache_path"])
	require.Equal(t, "~/.streamd.chat", cfg["chat_messages_storage"])

	gitRepo, ok := cfg["git_repo"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, gitRepo["enable"])
	require.Equal(t, "git@github.com:user/repo.git", gitRepo["url"])

	backends, ok := cfg["backends"].(map[string]any)
	require.True(t, ok)

	// Check Twitch
	twitchCfg, ok := backends["twitch"].(map[string]any)
	require.True(t, ok)
	accounts, ok := twitchCfg["accounts"].(map[string]any)
	require.True(t, ok)
	defaultAccount, ok := accounts["default"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "channel1", defaultAccount["channel"])
	require.Equal(t, "clientid1", defaultAccount["client_id"])
	require.Equal(t, true, defaultAccount["enable"])

	profiles, ok := defaultAccount["stream_profiles"].(map[string]any)
	require.True(t, ok)
	defaultProfiles, ok := profiles["default"].(map[string]any)
	require.True(t, ok)
	p1, ok := defaultProfiles["profile1"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "parent1", p1["parent"])

	orderV, ok := p1["order"]
	require.True(t, ok)
	require.Equal(t, uint64(10), toUint64(orderV))

	// Check OBS
	obsCfg, ok := backends["obs"].(map[string]any)
	require.True(t, ok)
	obsAccounts, ok := obsCfg["accounts"].(map[string]any)
	require.True(t, ok)
	obsDefault, ok := obsAccounts["default"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "localhost", obsDefault["host"])
}

func toUint64(v any) uint64 {
	switch i := v.(type) {
	case int:
		return uint64(i)
	case uint64:
		return i
	case int64:
		return uint64(i)
	default:
		return 0
	}
}
