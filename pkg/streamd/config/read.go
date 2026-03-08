package config

import (
	"bytes"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol"
)

var _ io.Reader = (*Config)(nil)
var _ io.ReaderFrom = (*Config)(nil)
var _ yaml.BytesUnmarshaler = (*Config)(nil)

func (cfg *Config) Read(
	b []byte,
) (int, error) {
	return len(b), cfg.UnmarshalYAML(b)
}

func (cfg *Config) traceDump() {
	l := logger.Default()
	if observability.LogLevelFilter.GetLevel() < logger.LevelTrace {
		return
	}
	if cfg == nil {
		l.Tracef("streamd config == nil")
		return
	}

	for idx, rule := range cfg.TriggerRules {
		l.Tracef("triggerRule[%d] == %s", idx, rule)
	}

	var buf bytes.Buffer
	_, err := cfg.WriteTo(&buf)
	if err != nil {
		l.Error(err)
		return
	}
	l.Tracef("streamd dump == %s", spew.Sdump(cfg))
	l.Tracef("streamd config == %#+v: %s", *cfg, buf.String())
}

func (cfg *Config) UnmarshalYAML(b []byte) (_err error) {
	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("got a panic: %v\n%s", r, debug.Stack())
		}
	}()
	logger.Default().Tracef("unparsed streamd config == %s", b)
	err := yaml.Unmarshal(b, (*config)(cfg))
	if err != nil {
		return fmt.Errorf("(Config reading) unable to unserialize data: %w", err)
	}
	cfg.traceDump()

	if cfg.Backends == nil {
		cfg.Backends = streamcontrol.Config{}
	}
	if cfg.Dashboard.Elements == nil {
		cfg.Dashboard.Elements = make(map[string]DashboardElementConfig)
	}

	return nil
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
