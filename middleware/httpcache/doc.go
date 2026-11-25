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
// A response is stored when its status is cacheable (200 by default), it has no
// Set-Cookie header, no Cache-Control no-store/private/no-cache directive, its
// body fits WithMaxBodySize and its Vary header only names headers configured
// with WithVaryHeaders.
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
// With WithWarmUpTTL the handler also runs in the background after the original
// request has completed. Flush is a no-op and Hijack is unsupported, so routes
// serving SSE or websockets must not use the middleware.
//
// # Keys and invalidation
//
// The key covers the method, host, escaped path, sorted query and configured
// vary headers, and is hashed so it is valid for every storage backend.
// Request headers that are not part of the key (for example X-Forwarded-Host)
// must not influence the response. Middleware.Key returns the key of a request,
// which can be passed to anycache.Cache.Invalidate.
//
// Response compression should be applied outside the cache middleware;
// otherwise add Accept-Encoding to the vary headers.
//
// Storage errors fail open: the request is served by the handler directly.
package httpcache
