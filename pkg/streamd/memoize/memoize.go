package memoize

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/immune-gmbh/attestation-sdk/pkg/objhash"
	"github.com/xaionaro-go/lockmap"
	"github.com/xaionaro-go/streamctl/pkg/clock"
)

type MemoizeData struct {
	Cache         map[objhash.ObjHash]any
	CacheMetaLock sync.Mutex
	CacheLockMap  *lockmap.LockMap
}

func NewMemoizeData() *MemoizeData {
	return &MemoizeData{
		Cache:        map[objhash.ObjHash]any{},
		CacheLockMap: lockmap.NewLockMap(),
	}
}

const timeFormat = time.RFC3339

func Memoize[REQ any, REPLY any, T func(context.Context, REQ) (REPLY, error)](
	d *MemoizeData,
	fn T,
	ctx context.Context,
	req REQ,
	cacheDuration time.Duration,
) (_ret REPLY, _err error) {
	logger.Tracef(ctx, "memoize %T:%#+v", req, req)
	defer logger.Tracef(ctx, "/memoize %T:%#+v", req, req)

	if IsNoCache(ctx) {
		logger.Tracef(ctx, "forcing no cache")
		cacheDuration = 0
	}

	key, err := objhash.Build(fmt.Sprintf("%T", req), req)
	errmon.ObserveErrorCtx(ctx, err)
	if err != nil {
		return fn(ctx, req)
	}
	ctx = belt.WithField(ctx, "cache_key", string(key[:]))
	ctx = belt.WithField(ctx, "cache_dur", cacheDuration)
	logger.Tracef(ctx, "req: %T:%#+v", req, req)

	d.CacheMetaLock.Lock()
	logger.Tracef(ctx, "grpc.CacheMetaLock.Lock()-ed")
	cache := d.Cache

	h := d.CacheLockMap.Lock(context.Background(), key)
	logger.Tracef(ctx, "grpc.CacheLockMap.Lock(%X)-ed", key[:])

	type cacheItem struct {
		Reply   REPLY
		Error   error
		SavedAt time.Time
	}

	if h.UserData != nil {
		if v, ok := h.UserData.(cacheItem); ok {
			d.CacheMetaLock.Unlock()
			logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
			h.Unlock()
			logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])
			logger.Tracef(ctx, "re-using the value")
			return v.Reply, v.Error
		} else {
			logger.Errorf(ctx, "cache-failure: expected type %T, but got %T", (*cacheItem)(nil), h.UserData)
		}
	}

	cachedResult, ok := cache[key]

	if ok {
		if v, ok := cachedResult.(cacheItem); ok {
			now := clock.Get().Now()
			cutoffTS := now.Add(-cacheDuration)
			if cacheDuration > 0 && !v.SavedAt.Before(cutoffTS) {
				d.CacheMetaLock.Unlock()
				logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
				h.UserData = nil
				h.Unlock()
				logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])
				logger.Tracef(ctx, "using the cached value")
				return v.Reply, v.Error
			}
			logger.Tracef(
				ctx,
				"the cached value expired: %s < %s (%s - %s)",
				v.SavedAt.Format(timeFormat),
				cutoffTS.Format(timeFormat),
				now.Format(timeFormat), cacheDuration,
			)
			delete(cache, key)
		} else {
			logger.Errorf(ctx, "cache-failure: expected type %T, but got %T", (*cacheItem)(nil), cachedResult)
		}
	}
	d.CacheMetaLock.Unlock()
	logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")

	var ts time.Time
	defer func() {
		cacheItem := cacheItem{
			Reply:   _ret,
			Error:   _err,
			SavedAt: ts,
		}
		h.UserData = cacheItem
		h.Unlock()
		logger.Tracef(ctx, "grpc.CacheLockMap.Unlock(%X)-ed", key[:])

		d.CacheMetaLock.Lock()
		logger.Tracef(ctx, "grpc.CacheMetaLock.Lock()-ed")
		defer logger.Tracef(ctx, "grpc.CacheMetaLock.Unlock()-ed")
		defer d.CacheMetaLock.Unlock()
		cache[key] = cacheItem
	}()

	logger.Tracef(ctx, "no cache")
	ts = clock.Get().Now()
	return fn(ctx, req)
}

func (d *MemoizeData) InvalidateCache(ctx context.Context) {
	logger.Debugf(ctx, "invalidateCache()")
	defer logger.Debugf(ctx, "/invalidateCache()")

	d.CacheMetaLock.Lock()
	defer d.CacheMetaLock.Unlock()

	d.CacheLockMap = lockmap.NewLockMap()
	d.Cache = map[objhash.ObjHash]any{}
}
