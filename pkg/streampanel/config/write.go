package config

import (
	"bytes"
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/datacounter"
)

var _ io.Writer = (*Config)(nil)
var _ io.WriterTo = (*Config)(nil)

func (cfg Config) Write(b []byte) (int, error) {
	n, err := cfg.WriteTo(bytes.NewBuffer(b))
	return int(n), err
}

func (cfg Config) WriteTo(
	w io.Writer,
) (int64, error) {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return 0, fmt.Errorf("unable to serialize data %#+v: %w", cfg, err)
	}

	counter := datacounter.NewWriterCounter(w)
	_, err = io.Copy(counter, bytes.NewReader(b))
	if err != nil {
		return int64(counter.Count()), fmt.Errorf("unable to write serialized config: %w", err)
	}
	return int64(counter.Count()), nil
}
