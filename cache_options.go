package anycache

import (
	"context"
	"time"
)

const (
	maxTTLShift = 100
)

// WithTTLRandomization sets the maximum TTL shift percentage.
// TTL randomization helps reduce cache stampedes by spreading expirations over time.
// Values above 100 are clamped to 100.
func WithTTLRandomization(shiftPercent uint8) func(*Cache) {
	return func(c *Cache) {
		if shiftPercent > maxTTLShift {
			c.maxShiftTTL = maxTTLShift

			return
		}

		c.maxShiftTTL = shiftPercent
	}
}

// WithKeyPrefix sets a default prefix applied to cache keys before storage.
func WithKeyPrefix(prefix string) func(*Cache) {
	return func(c *Cache) {
		c.keyPrefix = prefix
	}
}

// WithBaseContext sets the base context used for internal and background cache work.
func WithBaseContext(ctx context.Context) func(*Cache) {
	if ctx == nil {
		panic("base context cannot be nil")
	}

	return func(c *Cache) {
		c.ctx, c.cancelCtx = context.WithCancel(ctx)
	}
}

// WithMetricHook sets the default metrics hook for cache operations.
func WithMetricHook(hook func(key string, op State, latency time.Duration)) func(*Cache) {
	if hook == nil {
		panic("metric hook cannot be nil")
	}

	return func(c *Cache) {
		c.observer = hook
	}
}
