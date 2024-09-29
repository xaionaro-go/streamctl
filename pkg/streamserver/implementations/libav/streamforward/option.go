package streamforward

type Option interface {
	apply(*ActiveStreamForwarding)
}

type Options []Option

func (opts Options) apply(cfg *ActiveStreamForwarding) {
	for _, opt := range opts {
		opt.apply(cfg)
	}
}
