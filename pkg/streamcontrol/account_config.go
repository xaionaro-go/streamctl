package streamcontrol

type AccountConfigCommon interface {
	IsInitialized() bool
	IsEnabled() bool
}

type AccountConfigGeneric[SP StreamProfile] interface {
	AccountConfigCommon
	GetStreamProfiles() map[StreamID]StreamProfiles[SP]
}
