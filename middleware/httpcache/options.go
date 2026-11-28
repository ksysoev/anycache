package httpcache

import (
	"log/slog"
	"net/http"
	"net/textproto"
	"slices"
	"time"

	"github.com/ksysoev/anycache"
)

const (
	// DefaultMaxBodySize is the default maximum size of a stored response body.
	DefaultMaxBodySize = 1 << 20
	// DefaultTimeout bounds storage access and handler execution for a cache miss.
	DefaultTimeout = 30 * time.Second
)

// Option configures a Middleware.
type Option func(*config)

// config holds the middleware settings.
type config struct {
	logger          *slog.Logger
	keyFunc         func(*http.Request) (string, bool)
	ttlFunc         func(*http.Request) time.Duration
	cacheableStatus map[int]bool
	varySet         map[string]bool
	statusHeader    string
	varyHeaders     []string
	cacheOpts       []anycache.CacheItemOptions
	maxBodySize     int
	timeout         time.Duration
	allowAuthorized bool
	allowCookies    bool
	respectNoCache  bool
}

func defaultConfig() config {
	return config{
		logger:          slog.Default(),
		cacheableStatus: map[int]bool{http.StatusOK: true},
		varySet:         map[string]bool{},
		maxBodySize:     DefaultMaxBodySize,
		timeout:         DefaultTimeout,
	}
}

// WithKeyFunc replaces the default "method + scheme + host + path + sorted query" key.
// Returning ok=false bypasses the cache for that request.
// Configured vary headers are still appended to the returned key.
func WithKeyFunc(fn func(r *http.Request) (key string, ok bool)) Option {
	return func(c *config) {
		c.keyFunc = fn
	}
}

// WithVaryHeaders adds the values of the given request headers to the cache key.
// Responses with a Vary header naming a header outside this list are not cached.
func WithVaryHeaders(headers ...string) Option {
	return func(c *config) {
		for _, h := range headers {
			name := textproto.CanonicalMIMEHeaderKey(h)
			if c.varySet[name] {
				continue
			}

			c.varySet[name] = true
			c.varyHeaders = append(c.varyHeaders, name)
		}

		slices.Sort(c.varyHeaders)
	}
}

// WithCacheableStatus replaces the set of response status codes that are stored.
// By default only 200 responses are stored.
func WithCacheableStatus(codes ...int) Option {
	return func(c *config) {
		c.cacheableStatus = make(map[int]bool, len(codes))
		for _, code := range codes {
			c.cacheableStatus[code] = true
		}
	}
}

// WithTTLFunc computes a per-request TTL. A non-positive result falls back to the default TTL.
func WithTTLFunc(fn func(r *http.Request) time.Duration) Option {
	return func(c *config) {
		c.ttlFunc = fn
	}
}

// WithCacheStatusHeader enables a response header (for example "X-Cache") that
// reports HIT, MISS, WARM_UP or BYPASS.
func WithCacheStatusHeader(name string) Option {
	return func(c *config) {
		c.statusHeader = name
	}
}

// WithMaxBodySize sets the maximum size of a stored response body, which is
// also the most a response is buffered in memory (DefaultMaxBodySize by default).
// A larger response is streamed to the requesting client once it outgrows the
// limit and is neither stored nor shared. Zero or less disables the limit:
// every response is buffered in full and may be stored.
func WithMaxBodySize(n int) Option {
	return func(c *config) {
		c.maxBodySize = n
	}
}

// WithTimeout bounds storage access and handler execution on a cache miss.
//
// Storage access and the handler share the budget. If storage access times
// out, the request fails open and is served by the handler directly; a slow
// read that then misses leaves the handler only the rest of the timeout. If the handler is still running when the timeout expires,
// its context is cancelled, its output is discarded and the requests waiting for
// the response get 504 Gateway Timeout (503 when the cache is closing) at once.
// A handler that ignores its context keeps running in the background until it
// returns. The timeout also bounds a response streamed past WithMaxBodySize: if
// it expires mid-stream, the connection of the streaming request is aborted,
// so allow enough time for large downloads.
//
// Zero or less disables the middleware timeout; the shared work is then bounded
// only by the context deadline of the request that started it. Requests served
// by net/http have no deadline by default, so a hung handler then blocks every
// request waiting for the same key, with no limit.
//
// Storing a response may take up to one second past d, so a handler that takes
// most of the timeout is still cached.
//
// d also bounds the removal of a corrupt entry. A timeout passed through
// WithCacheOptions(anycache.WithTimeout(...)) applies to cache calls only, not
// to that removal.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithWarmUpTTL refreshes a cached response in the background when its
// remaining TTL drops below d, while still serving the cached response.
func WithWarmUpTTL(d time.Duration) Option {
	return WithCacheOptions(anycache.WithWarmUpTTL(d))
}

// WithCacheOptions passes additional anycache request options to every cache call.
func WithCacheOptions(opts ...anycache.CacheItemOptions) Option {
	return func(c *config) {
		c.cacheOpts = append(c.cacheOpts, opts...)
	}
}

// WithAllowAuthorized enables caching of requests carrying an Authorization header.
// Use it only with a key that is scoped to the user (see WithKeyFunc and WithVaryHeaders).
func WithAllowAuthorized() Option {
	return func(c *config) {
		c.allowAuthorized = true
	}
}

// WithAllowCookies enables caching of requests carrying a Cookie header.
// Use it only when responses do not depend on cookies, or vary on them.
func WithAllowCookies() Option {
	return func(c *config) {
		c.allowCookies = true
	}
}

// WithRespectRequestNoCache makes requests with "Cache-Control: no-cache" or
// "Pragma: no-cache" bypass the cache. It is disabled by default because it
// lets any client force requests through to the handler.
func WithRespectRequestNoCache() Option {
	return func(c *config) {
		c.respectNoCache = true
	}
}

// WithLogger sets the logger used for fail-open, panic, timeout and invalid
// status events. Defaults to slog.Default().
// anycache itself logs failures to store a response and failed WithWarmUpTTL
// refreshes via slog.Default(), so a failing refresh is logged by both.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		if logger != nil {
			c.logger = logger
		}
	}
}
