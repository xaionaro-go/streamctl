package types

type Server struct {
	Type   ServerType `yaml:"protocol"`
	Listen string     `yaml:"listen"`
}

type StreamConfig struct {
	Forwardings []DestinationID `yaml:"forwardings"`
}

type DestinationConfig struct {
	URL string `yaml:"url"`
}

type Config struct {
	Server       []Server                            `yaml:"servers"`
	Streams      map[StreamID]StreamConfig           `yaml:"streams"`
	Destinations map[DestinationID]DestinationConfig `yaml:"destinations"`
}
