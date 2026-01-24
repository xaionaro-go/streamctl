package streamcontrol

type PlatformConfig[AC AccountConfigGeneric[SP], SP StreamProfile] struct {
	Accounts map[AccountID]AC `yaml:"accounts,omitempty"`
	Custom   map[string]any   `yaml:"custom,omitempty"`
}

func (cfg *PlatformConfig[AC, SP]) IsInitialized() bool {
	if cfg == nil {
		return false
	}
	for _, account := range cfg.Accounts {
		if account.IsInitialized() {
			return true
		}
	}
	return false
}

func (cfg *PlatformConfig[AC, SP]) IsEnabled() bool {
	if cfg == nil {
		return false
	}
	for _, account := range cfg.Accounts {
		if account.IsEnabled() {
			return true
		}
	}
	return false
}

func (cfg *PlatformConfig[AC, SP]) SetCustomString(key string, value any) bool {
	if cfg == nil {
		return false
	}
	if cfg.Custom == nil {
		cfg.Custom = map[string]any{}
	}
	cfg.Custom[key] = value
	return true
}

func (cfg *PlatformConfig[AC, SP]) GetCustomString(key string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	if cfg.Custom == nil {
		return "", false
	}
	v, ok := cfg.Custom[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}
