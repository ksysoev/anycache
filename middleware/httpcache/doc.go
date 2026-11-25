// Package httpcache provides an HTTP server caching middleware backed by anycache.
//
// The middleware has the standard func(http.Handler) http.Handler shape and works
// with net/http, chi, gorilla and similar routers:
//
//	cache := anycache.New(store)
//
//	mw, err := httpcache.New(cache, time.Minute,
//		httpcache.WithVaryHeaders("Accept"),
//		httpcache.WithCacheStatusHeader("X-Cache"),
//	)
//	if err != nil {
//		return err
//	}
//
//	mux.Handle("GET /products", mw.Handler(productsHandler))
//
// # What is cached
//
// Only GET requests are cached. Requests with a Range, Authorization or Cookie
// header bypass the cache by default (see WithAllowAuthorized and WithAllowCookies).
// Upgrade requests (websockets) and requests accepting text/event-stream (SSE)
// always bypass it.
// A response is stored when its status is cacheable (200 by default), it has no
// Set-Cookie header, no Cache-Control no-store/private/no-cache directive, its
// body fits WithMaxBodySize and its Vary header only names headers configured
// with WithVaryHeaders. Response freshness (max-age, s-maxage, Expires) is
// ignored: stored responses live for the configured TTL or WithTTLFunc.
//
// # Request collapsing
//
// Concurrent identical requests run the handler once and receive the same
// response, including non-cacheable statuses such as 500. Private responses
// (see above) are never shared: other requests run the handler themselves.
//
// # Handler execution
//
// The handler runs on a copy of the request with no body and without
// conditional headers (If-None-Match, If-Modified-Since, ...), so it always
// produces a full response. Its context keeps the request values but is not
// cancelled when the client disconnects; it is bounded by WithTimeout instead.
// When the timeout expires, the waiting requests get a 504 response at once (503
// when the cache is shutting down) and whatever the handler wrote is discarded,
// never stored. A handler that ignores its context keeps running in the
// background until it returns, so cached handlers should respect r.Context(). A storage access that times out fails open.
// Trailers are not supported and are dropped, and 101 Switching Protocols
// responses are never stored or shared.
//
// The handler may run after the request that triggered it has completed: during
// a WithWarmUpTTL background refresh, or when that client disconnected while
// other requests wait for the response. Request-scoped resources reachable
// through the context (database transactions, pooled buffers, spans ended by an
// outer middleware) may already be released by then and must not be used by
// cached handlers; context values that stay valid (auth claims, request IDs,
// loggers) are fine.
//
// # Streaming and memory
//
// Responses are buffered in memory until the handler returns: Flush is a no-op
// and Hijack is unsupported. Upgrade and event-stream requests bypass the cache,
// but routes serving SSE or websockets should still not use the middleware. WithMaxBodySize only controls whether a response is stored;
// larger responses are still buffered in full, so do not wrap routes serving
// large downloads or files.
//
// # Keys and invalidation
//
// The key covers the method, scheme, host, escaped path, sorted query and
// configured vary headers, and is hashed so it is valid for every storage
// backend. The scheme is https only when the request arrived over TLS, so behind
// a TLS-terminating proxy use WithVaryHeaders("X-Forwarded-Proto") or
// WithKeyFunc to keep schemes apart. Query parameters are sorted by name but
// otherwise keyed as sent, without decoding.
// Request headers that are not part of the key (for example X-Forwarded-Host)
// must not influence the response. Middleware.Key returns the key of a request,
// which can be passed to anycache.Cache.Invalidate.
//
// Response compression should be applied outside the cache middleware;
// otherwise add Accept-Encoding to the vary headers.
//
// Storage errors fail open: when reading fails the request is served by the
// handler directly, when storing fails the response is still served and shared.
package httpcache
