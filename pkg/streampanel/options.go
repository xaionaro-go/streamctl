package streampanel

type configT struct {
}

func defaultConfig() configT {
	return configT{}
}

type Option interface {
	apply(cfg *configT)
}

type Options []Option

func (options Options) Config() configT {
	cfg := defaultConfig()
	for _, option := range options {
		option.apply(&cfg)
	}
	return cfg
}
