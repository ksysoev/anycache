package anycache

import "time"

// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func WithWarmUpTTL(ttl time.Duration) CacheItemOptions {
	return func(req *Request) {
		req.WarmUpTTL = ttl
	}
}

// WithMetric overrides the default metric hook for this cache request;
// the hook is called for each cache operation with its state and latency.
func WithMetric(hook func(key string, op State, latency time.Duration)) CacheItemOptions {
	if hook == nil {
		panic("metric hook cannot be nil")
	}

	return func(req *Request) {
		req.MetricHook = hook
	}
}

// WithTimeout sets a timeout for the internal cache work (storage + generation).
// When set, the internal work runs on the cache base context (see WithBaseContext),
// so caller cancellation and context values are not propagated to storage/generator.
func WithTimeout(timeout time.Duration) CacheItemOptions {
	return func(req *Request) {
		req.Timeout = timeout
	}
}
