package upgradeconfig

import (
	"fmt"
	"maps"

	"github.com/goccy/go-yaml"
)

func ConvertConfigBytes(b []byte) ([]byte, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("unable to unmarshal YAML: %w", err)
	}

	modified := false
	backends, ok := raw["backends"].(map[string]any)
	if ok {
		for _, platCfgRaw := range backends {
			platCfg, ok := platCfgRaw.(map[string]any)
			if !ok {
				continue
			}

			// Check if it's the old format: has 'config', 'stream_profiles', or 'enable' at platform level
			_, hasConfig := platCfg["config"]
			_, hasProfiles := platCfg["stream_profiles"]
			_, hasEnable := platCfg["enable"]
			_, hasAccounts := platCfg["accounts"]

			if (hasConfig || hasProfiles || hasEnable) && !hasAccounts {
				modified = true
				account := make(map[string]any)

				// Move 'enable'
				if hasEnable {
					account["enable"] = platCfg["enable"]
					delete(platCfg, "enable")
				}

				// Move 'config' fields
				if hasConfig {
					configMap, ok := platCfg["config"].(map[string]any)
					if ok {
						maps.Copy(account, configMap)
					}
					delete(platCfg, "config")
				}

				// Move 'stream_profiles'
				if hasProfiles {
					profilesMap, ok := platCfg["stream_profiles"].(map[string]any)
					if ok {
						account["stream_profiles"] = map[string]any{
							"default": profilesMap,
						}
					}
					delete(platCfg, "stream_profiles")
				}

				platCfg["accounts"] = map[string]any{
					"default": account,
				}
			}
		}
	}

	if !modified {
		return b, nil
	}

	return yaml.Marshal(raw)
}
