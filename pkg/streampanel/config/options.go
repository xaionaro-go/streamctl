package config

type Option interface {
	Apply(cfg *Config)
}

type Options []Option

func (options Options) ApplyOverrides(cfg Config) Config {
	for _, option := range options {
		option.Apply(&cfg)
	}
	return cfg
}

type OptionRemoteStreamDAddr string

func (o OptionRemoteStreamDAddr) Apply(cfg *Config) {
	cfg.RemoteStreamDAddr = string(o)
}
