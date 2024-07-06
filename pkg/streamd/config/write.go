package config

import (
	"bytes"
	"fmt"
	"io"

	goyaml "github.com/go-yaml/yaml"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/datacounter"
)

var _ io.Writer = (*Config)(nil)
var _ io.WriterTo = (*Config)(nil)
var _ yaml.BytesMarshaler = (*Config)(nil)

func (cfg Config) Write(b []byte) (int, error) {
	n, err := cfg.WriteTo(bytes.NewBuffer(b))
	return int(n), err
}

func (cfg Config) WriteTo(
	w io.Writer,
) (int64, error) {
	b, err := cfg.MarshalYAML()
	if err != nil {
		return 0, err
	}

	counter := datacounter.NewWriterCounter(w)
	io.Copy(counter, bytes.NewReader(b))
	return int64(counter.Count()), nil
}

func (cfg Config) MarshalYAML() ([]byte, error) {
	b, err := yaml.Marshal((config)(cfg))
	if err != nil {
		return nil, fmt.Errorf("unable to serialize data %#+v: %w", cfg, err)
	}

	// have to use another YAML encoder to avoid the random-indent bug,
	// but also have to use the initial encoder to correctly map
	// out structures to YAML; so using both sequentially :(

	m := map[string]any{}
	err = goyaml.Unmarshal(b, &m)
	if err != nil {
		return nil, fmt.Errorf("unable to unserialize data %#+v: %w", cfg, err)
	}

	b, err = goyaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("unable to re-serialize data %#+v: %w", cfg, err)
	}

	return b, nil
}
