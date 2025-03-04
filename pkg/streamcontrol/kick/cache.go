package kick

import (
	"context"
	"sync/atomic"
	"unsafe"

	"github.com/xaionaro-go/kickcom"
)

type Cache struct {
	ChanInfo *kickcom.ChannelV1
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

func (cache *Cache) GetChanInfo() *kickcom.ChannelV1 {
	if cache == nil {
		return nil
	}

	return (*kickcom.ChannelV1)(atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&cache.ChanInfo))))
}

func (cache *Cache) SetChanInfo(chanInfo *kickcom.ChannelV1) {
	atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&cache.ChanInfo)), unsafe.Pointer(chanInfo))
}
