package types

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (options Options) Apply(cfg Config) Config {
	for _, option := range options {
		option.Apply(&cfg)
	}
	return cfg
}

type OptionPathToMPV string

func (opt OptionPathToMPV) Apply(cfg *Config) {
	cfg.PathToMPV = string(opt)
}
