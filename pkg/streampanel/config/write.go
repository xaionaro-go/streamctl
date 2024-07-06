package config

import (
	"bytes"
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
		return 0, err
	}

	counter := datacounter.NewWriterCounter(w)
	io.Copy(counter, bytes.NewReader(b))
	return int64(counter.Count()), nil
}
