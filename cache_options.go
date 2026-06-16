package anycache

// WithTTLRandomization sets the maximum TTL shift percentage.
// TTL randomization helps prevent cache stampedes by spreading entry expirations over time.
// Note: the current implementation shifts TTL within roughly ±(maxShiftPercent/2)% of the original TTL.
func WithTTLRandomization(maxShiftPercent uint8) func(*Cache) {
	return func(c *Cache) {
		c.maxShiftTTL = maxShiftPercent
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
