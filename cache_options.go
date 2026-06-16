package anycache

// WithTTLRandomization sets max shift of TTL in persent
// TTL randomization is a technique used to prevent cache stampedes by adding a random shift to the TTL (time-to-live) of cache entries.
// This helps to distribute the expiration times of cache entries more evenly,
// reducing the likelihood of multiple entries expiring at the same time and causing a surge in traffic to the underlying data source.
// The maxShiftPercent parameter specifies the maximum percentage by which the TTL can be shifted randomly. For example,
// if maxShiftPercent is set to 20, then the TTL of a cache entry can be randomly shifted by up to 20% of its original value.
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
