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

func (cfg Config) Write(b []byte) (int, error) {
	n, err := cfg.WriteTo(bytes.NewBuffer(b))
	return int(n), err
}

func (cfg Config) WriteTo(
	w io.Writer,
) (int64, error) {
	// There is bug in github.com/goccy/go-yaml that makes wrong intention
	// in cfg.BuiltinStreamD.GitRepo.PrivateKey makes the whole value unparsable
	//
	// Working this around...
	key := cfg.BuiltinStreamD.GitRepo.PrivateKey
	cfg.BuiltinStreamD.GitRepo.PrivateKey = ""

	b, err := yaml.Marshal(cfg)
	if err != nil {
		return 0, fmt.Errorf("unable to serialize data %#+v: %w", cfg, err)
	}

	m := map[any]any{}
	err = goyaml.Unmarshal(b, &m)
	if err != nil {
		return 0, fmt.Errorf("unable to unserialize data %s: %w", b, err)
	}
	if v, ok := m["streamd_builtin"]; ok {
		if m2, ok := v.(map[any]any); ok {
			if v, ok := m2["gitrepo"]; ok {
				if m3, ok := v.(map[any]any); ok {
					m3["private_key"] = key
				}
			}
		}
	}

	b, err = goyaml.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("unable to re-serialize data %#+v: %w", m, err)
	}

	counter := datacounter.NewWriterCounter(w)
	io.Copy(counter, bytes.NewReader(b))
	return int64(counter.Count()), nil
}
