package anycache

import "time"

// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func WithWarmUpTTL(ttl time.Duration) CacheItemOptions {
	return func(req *CacheReuest) {
		req.WarmUpTTL = ttl
	}
}

// WithMetric override default metric hook for this cache request with function to be called for each cache operation, providing metrics such as operation type and latency.
func WithMetric(hook func(key string, op State, latency time.Duration)) CacheItemOptions {
	if hook == nil {
		panic("metric hook cannot be nil")
	}

	return func(req *CacheReuest) {
		req.MetricHook = hook
	}
}
