package anycache

import "time"

// WithWarmUpTTL sets the TTL threshold that triggers asynchronous warm-up refresh.
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

// WithTimeout sets an explicit timeout for internal cache work (storage + generation).
// Internal shared work uses the cache base context (see WithBaseContext);
// caller cancellation/context values are decoupled from storage/generator execution.
// Storing a generated value may take up to one second longer (at most the
// timeout itself), so a generator finishing close to the deadline does not leave
// the storage write without time. A value returned after the timeout expired
// may be partial and is not stored.
func WithTimeout(timeout time.Duration) CacheItemOptions {
	return func(req *Request) {
		req.Timeout = timeout
	}
}

// WithShouldCache sets a predicate that decides whether the generated value should be cached.
// The function takes the generated value as input and
// returns true to cache it or false to skip caching.
// Requests using this option are not de-duplicated. It is ignored by CacheC,
// where the generator itself reports whether a value is cacheable.
func WithShouldCache(shouldCache func(any) bool) CacheItemOptions {
	if shouldCache == nil {
		panic("shouldCache function cannot be nil")
	}

	return func(req *Request) {
		req.shouldCache = shouldCache
	}
}
