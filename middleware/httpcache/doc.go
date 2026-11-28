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
// background until it returns, so cached handlers should respect r.Context().
// The storage read shares the timeout with the handler: a read that times out
// fails open, but a slow read that then misses leaves the handler only the rest
// of the timeout. A handler panic produces a 500 response, and a handler that
// aborts with http.ErrAbortHandler produces a 502: a response within
// WithMaxBodySize is buffered, so nothing partial ever reached the client. Once
// an oversized response started streaming (see below), a panic, abort or
// timeout aborts the connection of the request that ran the handler instead,
// with panic(http.ErrAbortHandler), so the client never takes the partial body
// for a complete one. The other waiting requests were already released when the
// response outgrew the limit and run the handler themselves.
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
// Responses are buffered in memory until the handler returns, up to
// WithMaxBodySize. When a response outgrows the limit, the buffered part and the
// rest of the body are streamed to the request that ran the handler, with the
// BYPASS cache status, the handler's headers (its Content-Length if it set one,
// chunked encoding otherwise) and working Flush; the response is neither stored
// nor shared: concurrent requests waiting for it are released as soon as it
// outgrows the limit and run the handler themselves, also streaming, so a slow
// client never holds them. Memory per response therefore stays bounded by
// WithMaxBodySize plus the write in progress, so routes serving large downloads
// can be wrapped. WithTimeout still bounds the whole handler run, streaming
// included, so raise it for slow downloads; when it expires, or the client goes
// away, mid-stream, the handler context is cancelled and the connection aborted,
// even when the client stopped reading. Closing the cache does not cancel a
// response that is already streaming. When the response has no client to
// stream to (a WithWarmUpTTL refresh, or a client that went away) the oversized
// body is discarded and the handler's writes fail, so it can stop early; a
// handler aborting with http.ErrAbortHandler on such a failed write (as
// httputil.ReverseProxy does) is not reported as a 502 to the waiting requests,
// which run the handler themselves.
// Before the limit is reached Flush is a no-op, and Hijack is unsupported.
// Upgrade and event-stream requests bypass the cache, but routes serving SSE or
// websockets should still not use the middleware.
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
