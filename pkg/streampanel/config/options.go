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

type OptionOAuthListenPortTwitch uint16

func (o OptionOAuthListenPortTwitch) Apply(cfg *Config) {
	cfg.OAuth.ListenPorts.Twitch = uint16(o)
}

type OptionOAuthListenPortYouTube uint16

func (o OptionOAuthListenPortYouTube) Apply(cfg *Config) {
	cfg.OAuth.ListenPorts.YouTube = uint16(o)
}
