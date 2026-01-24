package cache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/goccy/go-yaml"
	"github.com/nicklaw5/helix/v2"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/kick"
	"github.com/xaionaro-go/streamctl/pkg/streamcontrol/youtube"
)

type Kick = kick.Cache

type Twitch struct {
	Categories []helix.Game
}

func (c *Twitch) Clone() *Twitch {
	return &Twitch{
		Categories: c.Categories,
	}
}

type YouTube struct {
	Broadcasts []*youtube.LiveBroadcast
}

func (c *YouTube) Clone() *YouTube {
	return &YouTube{
		Broadcasts: c.Broadcasts,
	}
}

type Cache struct {
	PerPlatform map[string]any `yaml:",inline"`
}

func (c *Cache) Clone() *Cache {
	if c == nil {
		return nil
	}
	result := &Cache{
		PerPlatform: make(map[string]any),
	}
	for k, v := range c.PerPlatform {
		result.PerPlatform[k] = v // This is a shallow copy of the any value
	}
	return result
}

func ReadCacheFromPath(
	ctx context.Context,
	cfgPath string,
	cache *Cache,
) (_err error) {
	defer func() { logger.Tracef(ctx, "/ReadCacheFromPath result: %v", _err) }()
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("unable to read file '%s': %w", cfgPath, err)
	}

	return ReadCache(ctx, b, cache)
}

func ReadCache(
	ctx context.Context,
	b []byte,
	cache *Cache,
) error {
	err := yaml.Unmarshal(b, cache)
	if err != nil {
		return fmt.Errorf("unable to unserialize data: %w: <%s>", err, b)
	}

	return nil
}

func WriteCacheToPath(
	ctx context.Context,
	cfgPath string,
	cache Cache,
) (_err error) {
	defer func() { logger.Tracef(ctx, "/WriteCacheToPath result: %v", _err) }()
	pathNew := cfgPath + ".new"
	f, err := os.OpenFile(pathNew, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0750)
	if err != nil {
		return fmt.Errorf("unable to open the data file '%s': %w", pathNew, err)
	}
	err = WriteCache(ctx, f, cache)
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to write data to file '%s': %w", pathNew, err)
	}
	err = os.Rename(pathNew, cfgPath)
	if err != nil {
		return fmt.Errorf("cannot move '%s' to '%s': %w", pathNew, cfgPath, err)
	}
	logger.Infof(ctx, "wrote to '%s' Cache %#+v", cfgPath, cache)
	return nil
}

func WriteCache(
	_ context.Context,
	w io.Writer,
	cache Cache,
) error {
	b, err := yaml.Marshal(cache)
	if err != nil {
		return fmt.Errorf("unable to serialize data %#+v: %w", cache, err)
	}

	_, err = io.Copy(w, bytes.NewBuffer(b))
	if err != nil {
		return fmt.Errorf("unable to write data %#+v: %w", cache, err)
	}
	return nil
}
