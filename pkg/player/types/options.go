package types

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (options Options) Config() Config {
	cfg := Config{}
	options.Apply(&cfg)
	return cfg
}

func (options Options) Apply(cfg *Config) {
	for _, option := range options {
		option.Apply(cfg)
	}
}

type OptionPathToMPV string

func (opt OptionPathToMPV) Apply(cfg *Config) {
	cfg.PathToMPV = string(opt)
}
