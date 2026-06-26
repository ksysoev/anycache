package anycache

import (
	"context"
	"time"
)

const (
	maxTTLShift = 100
)

// WithTTLRandomization sets the maximum TTL shift percentage.
// TTL randomization helps prevent cache stampedes by spreading entry expirations over time.
// maximum shift percentage is 100, which means that TTL can be shifted by up to 100% of the original TTL value.
func WithTTLRandomization(shiftPercent uint8) func(*Cache) {
	return func(c *Cache) {
		if shiftPercent > maxTTLShift {
			c.maxShiftTTL = maxTTLShift

			return
		}

		c.maxShiftTTL = shiftPercent
	}
}

// WithKeyPrefix sets default key prefix for cache keys
// Key prefix is a string that is added to the beginning of cache keys before they are stored in the cache.
// This can be useful for namespacing cache entries, preventing key collisions, and organizing cache data.
func WithKeyPrefix(prefix string) func(*Cache) {
	return func(c *Cache) {
		c.keyPrefix = prefix
	}
}

// WithBaseContext sets the base context for cache operations,
// allowing a custom base context for background tasks such as warming up cache entries.
func WithBaseContext(ctx context.Context) func(*Cache) {
	if ctx == nil {
		panic("base context cannot be nil")
	}

	return func(c *Cache) {
		c.ctx, c.cancelCtx = context.WithCancel(ctx)
	}
}

// WithMetricHook sets a default hook function to be called for each cache operation,
// providing metrics such as operation type and latency.
func WithMetricHook(hook func(key string, op State, latency time.Duration)) func(*Cache) {
	if hook == nil {
		panic("metric hook cannot be nil")
	}

	return func(c *Cache) {
		c.observer = hook
	}
}
