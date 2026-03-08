package youtube

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGoogleQuotaCacheExpiry(t *testing.T) {
	fakeNow := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	cache := &googleQuotaCache{
		nowFunc: func() time.Time { return fakeNow },
	}

	// Empty cache is expired
	assert.True(t, cache.isExpired())

	// Set values
	usage := uint64(500)
	limit := uint64(10000)
	cache.set(usage, limit, fakeNow)

	// Fresh cache is not expired
	assert.False(t, cache.isExpired())

	// Check values
	u, l, at, ok := cache.get()
	assert.True(t, ok)
	assert.Equal(t, uint64(500), u)
	assert.Equal(t, uint64(10000), l)
	assert.Equal(t, fakeNow, at)
}

func TestGoogleQuotaCacheExpiry_Deterministic(t *testing.T) {
	fakeNow := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := &googleQuotaCache{
		nowFunc: func() time.Time { return fakeNow },
	}

	cache.set(100, 10000, fakeNow)
	assert.False(t, cache.isExpired())

	// Advance past TTL
	fakeNow = fakeNow.Add(googleQuotaCacheTTL + time.Second)
	assert.True(t, cache.isExpired())
}
