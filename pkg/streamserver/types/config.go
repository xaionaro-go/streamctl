package types

type Server struct {
	Type   ServerType `yaml:"protocol"`
	Listen string     `yaml:"listen"`
}

type ForwardingConfig struct {
	Disabled bool `yaml:"disabled,omitempty"`
}

type StreamConfig struct {
	Forwardings map[DestinationID]ForwardingConfig `yaml:"forwardings"`
}

type DestinationConfig struct {
	URL string `yaml:"url"`
}

type Config struct {
	Servers      []Server                             `yaml:"servers"`
	Streams      map[StreamID]*StreamConfig           `yaml:"streams"`
	Destinations map[DestinationID]*DestinationConfig `yaml:"destinations"`
}
