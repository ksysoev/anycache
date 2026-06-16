package anycache

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
