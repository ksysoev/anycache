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

// WithTimeout decouples cache request processing from the incoming context so that
// cancellations on the incoming request do not affect other callers waiting for the same key to be generated.
// It sets a timeout for the cache request, after which the request will be canceled if not completed.
func WithTimeout(timeout time.Duration) CacheItemOptions {
	return func(req *Request) {
		req.Timeout = timeout
	}
}
