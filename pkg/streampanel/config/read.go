package config

import (
	"fmt"
	"io"

	"github.com/goccy/go-yaml"
)

var _ io.Reader = (*Config)(nil)
var _ io.ReaderFrom = (*Config)(nil)

func (cfg *Config) Read(
	b []byte,
) (int, error) {
	return len(b), yaml.Unmarshal(b, cfg)
}

func (cfg *Config) ReadFrom(
	r io.Reader,
) (int64, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return int64(len(b)), fmt.Errorf("unable to read: %w", err)
	}

	n, err := cfg.Read(b)
	return int64(n), err
}
