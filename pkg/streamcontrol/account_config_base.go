package streamcontrol

type AccountConfigBase[SP StreamProfile] struct {
	Enable         *bool                           `yaml:"enable,omitempty"`
	StreamProfiles map[StreamID]StreamProfiles[SP] `yaml:"stream_profiles,omitempty"`
}

func (cfg AccountConfigBase[SP]) IsEnabled() bool {
	if cfg.Enable == nil {
		return false
	}
	return *cfg.Enable
}

func (cfg *AccountConfigBase[SP]) SetEnabled(enabled bool) {
	cfg.Enable = &enabled
}

func (cfg AccountConfigBase[SP]) GetStreamProfiles() map[StreamID]StreamProfiles[SP] {
	return cfg.StreamProfiles
}
