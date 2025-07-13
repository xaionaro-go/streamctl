package kick

import (
	"context"

	"github.com/scorfly/gokick"
)

type Cache struct {
	ChanInfo   *gokick.ChannelResponse
	Categories *[]gokick.CategoryResponse
}

func (c *Cache) Clone() *Cache {
	return &Cache{
		ChanInfo:   c.GetChanInfo(),
		Categories: ptr(c.GetCategories()),
	}
}

type ctxKeyCacheT struct{}

var ctxKeyCache ctxKeyCacheT

func CtxWithCache(ctx context.Context, cache *Cache) context.Context {
	return context.WithValue(ctx, ctxKeyCache, cache)
}

func CacheFromCtx(ctx context.Context) *Cache {
	cacheIface := ctx.Value(ctxKeyCache)
	cache, _ := cacheIface.(*Cache)
	return cache
}
