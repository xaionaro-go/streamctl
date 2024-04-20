package streampanel

type config struct {
}

func defaultConfig() config {
	return config{}
}

type Option interface {
	apply(cfg *config)
}

type Options []Option

func (options Options) Config() config {
	cfg := defaultConfig()
	for _, option := range options {
		option.apply(&cfg)
	}
	return cfg
}
