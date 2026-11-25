# AnyCache

[![tests](https://github.com/ksysoev/anycache/actions/workflows/main.yml/badge.svg)](https://github.com/ksysoev/anycache/actions/workflows/main.yml)
[![codecov](https://codecov.io/gh/ksysoev/anycache/branch/main/graph/badge.svg?token=J7936BN4R2)](https://codecov.io/gh/ksysoev/anycache)
[![Go Report Card](https://goreportcard.com/badge/github.com/ksysoev/anycache)](https://goreportcard.com/report/github.com/ksysoev/anycache)
[![Go Reference](https://pkg.go.dev/badge/github.com/ksysoev/anycache.svg)](https://pkg.go.dev/github.com/ksysoev/anycache)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

`anycache` is a Go cache-aside helper that wraps expensive reads with a consistent API across storage backends.

It is built for teams that want to add caching quickly without reimplementing stampede protection, refresh-on-near-expiry behavior, and cache lifecycle wiring in every service.

### Why use anycache

- Reduce repeated backend work with lazy, on-demand caching.
- Deduplicate concurrent misses for the same key.
- Keep hot keys fresh with optional warm-up before TTL expiry.
- Switch storage backends (Redis, in-memory, layered, and more) without changing calling code.

### Who it is for

- Go services that use cache-aside patterns around DB/API calls.
- Teams that want a small, explicit caching abstraction instead of custom one-off wrappers.

## Installation

```bash
go get github.com/ksysoev/anycache
```

## Quick Start (Redis)

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ksysoev/anycache"
	redisstorage "github.com/ksysoev/anycache/storage/redis"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer func() { _ = rdb.Close() }()

	cache := anycache.New(redisstorage.New(rdb))
	defer func() { _ = cache.Close() }()

	data, err := cache.Cache(ctx, "user:42", 5*time.Minute, func(context.Context) ([]byte, error) {
		return []byte("cached value"), nil
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(string(data))
}
```

## Core APIs

- `Cache(ctx, key, ttl, generator, opts...) ([]byte, error)` for raw `[]byte`
- `CacheS(ctx, key, ttl, generator, opts...) (string, error)` for string values
- `CacheStruct(ctx, key, ttl, generator, result, opts...) error` for JSON-serialized structs
- `CacheC(ctx, key, ttl, generator, opts...) ([]byte, error)` where the generator returns `(data, cacheable, err)`; non-cacheable results are returned but not stored, and concurrent requests stay de-duplicated. A failure to store the value is logged instead of returned, so callers still get the generated value
- `Invalidate(ctx, key) error` to remove a key
- `Close() error` to stop background work gracefully

```go
// CacheS
name, err := cache.CacheS(ctx, "user:name", time.Minute, func(context.Context) (string, error) {
	return "alice", nil
})
if err != nil {
	panic(err)
}
_ = name

// CacheStruct
type Profile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var p Profile
if err := cache.CacheStruct(ctx, "user:profile", 5*time.Minute, func(context.Context) (any, error) {
	return Profile{ID: 42, Name: "Alice"}, nil
}, &p); err != nil {
	panic(err)
}

// Invalidate
if err := cache.Invalidate(ctx, "user:profile"); err != nil {
	panic(err)
}
```

## Behavior & options

### Cache-level options

- `WithTTLRandomization(percent)` — spread expirations to reduce stampedes.
- `WithKeyPrefix(prefix)` — namespace keys.
- `WithBaseContext(ctx)` — set base context for internal/background work.
- `WithMetricHook(func(key string, op anycache.State, latency time.Duration))` — default per-request metric hook.
- `WithCodec(codec)` — override the default JSON codec for `CacheStruct`. AnyCache provides codecs for JSON, MessagePack, BSON, and Gob formats. See `codec/*` for details.

### Request-level options

- `WithWarmUpTTL(d)` — if remaining TTL is below `d`, serve current value and refresh in background.
- `WithMetric(hook)` — override metric hook for one call.
- `WithTimeout(d)` — timeout for internal storage + generation work. Storing a generated value may take up to 1s past it.
- `WithShouldCache(func(any) bool)` — decide per result whether to persist generated data.

Metric states: `hit`, `miss`, `warm_up`, `error`.

## Important semantics

- **Singleflight dedupe scope:** concurrent requests for the same key are deduplicated within a single `anycache.Cache` instance.
- **Warm-up behavior (`WithWarmUpTTL`):** when a key exists and its remaining TTL is `> 0` and `<= warmUpTTL`, anycache returns the current cached value immediately and schedules a background refresh.
- **Warm-up lock semantics:** only one warm-up refresh per key is started at a time; concurrent requests do not start duplicate warm-up goroutines.
- **Context model for deduplicated requests:** singleflight-backed requests run storage/generator work on the cache base context (default or `WithBaseContext`) so one caller cancellation does not abort shared work for the same key.
- **Timeout behavior (`WithTimeout` + caller deadlines):** `WithTimeout` sets an explicit timeout for internal work. If no explicit timeout is set and the initiating caller context has a deadline, that deadline is mirrored as an internal timeout for the shared work.
- **Conditional caching (`CacheC`):** the generator decides per result whether it is stored. Unlike `WithShouldCache`, concurrent callers still share one generator execution and all receive its result, stored or not.
- **Conditional caching (`WithShouldCache`):** the predicate receives the original generator value type used by the API call (`[]byte` for `Cache`, `string` for `CacheS`, and the original struct/object for `CacheStruct`). Return `false` to skip storing while still returning generated data.
- **Caller cancellation expectations:** caller cancellation still stops waiting for that caller, but internal shared work continues on the cache base context.
- **Lifecycle (`Close`):** call `Close()` during shutdown to cancel background work and wait for in-flight warm-up goroutines to finish.

## When to use anycache

Use anycache when you want:

- A consistent cache-aside API for expensive reads in Go services.
- Built-in same-key deduplication to reduce thundering-herd/stampede pressure.
- Optional warm-up refresh behavior without writing custom background orchestration.
- Flexibility to move between in-memory, Redis, layered, or other supported backends.

## When not to use it

anycache may be a poor fit when:

- You need backend-specific features directly (for example advanced Redis primitives) as part of core logic.
- Your use case is very small and a direct one-off cache-aside wrapper is simpler to maintain.
- You require highly custom invalidation/orchestration rules that sit outside this abstraction.

## Alternatives

- **Direct backend client:** maximum control, but you manage dedupe, warm-up, and consistency details yourself.
- **Hand-rolled cache-aside wrapper:** can work for narrow use cases, but tends to duplicate behavior across services over time.

## Release notes

See [GitHub Releases](https://github.com/ksysoev/anycache/releases) for release notes and version-to-version changes. Tags are available at [GitHub Tags](https://github.com/ksysoev/anycache/tags).

## HTTP caching middleware

`github.com/ksysoev/anycache/middleware/httpcache` caches responses of HTTP handlers. It uses only the standard library and has the usual `func(http.Handler) http.Handler` shape, so it works with `net/http`, chi, gorilla and similar routers.

```go
cache := anycache.New(store)

mw, err := httpcache.New(cache, time.Minute,
	httpcache.WithVaryHeaders("Accept"),
	httpcache.WithCacheStatusHeader("X-Cache"), // HIT | MISS | WARM_UP | BYPASS
	httpcache.WithWarmUpTTL(10*time.Second),
)
if err != nil {
	return err
}

mux.Handle("GET /products", mw.Handler(productsHandler))
```

What gets cached:

- Only `GET` requests. Requests with `Range`, `Authorization` or `Cookie` headers bypass the cache (opt in with `WithAllowAuthorized()` / `WithAllowCookies()`, using a user-scoped key).
- The key is the method, scheme, host, path, sorted query and the headers named in `WithVaryHeaders`, hashed so it is valid for every backend (including memcache). Query parameters are sorted by name but otherwise keyed exactly as sent, so `?a` and `?a=` are different keys. Override it with `WithKeyFunc`. Behind a TLS-terminating proxy every request is `http`; add `X-Forwarded-Proto` to the vary headers if responses differ per scheme.
- A response is stored only if its status is cacheable (`200` by default, see `WithCacheableStatus`), it has no `Set-Cookie`, no `Cache-Control: no-store/private/no-cache`, its body fits `WithMaxBodySize` (1 MiB by default), and its `Vary` header only names configured vary headers.

Behavior:

- **Request collapsing:** concurrent identical requests run the handler once and share the response, even when it is not stored (for example a `500`). Private responses (`Set-Cookie`, `private`, unsafe `Vary`, oversized) are never shared; the other requests run the handler themselves.
- **Handler context:** the handler runs on a copy of the request without body and conditional headers, so it always returns a full response. Its context keeps the request values (auth, tracing, loggers) but is not cancelled when the client disconnects.
- **Timeout:** storage access and handler execution are bounded by `WithTimeout` (30s by default). A storage timeout fails open. A handler still running when it expires (or panicking because its context expired) produces `504 Gateway Timeout` (`503` when the cache is closing) for all waiting requests, and its output is never stored. With `WithTimeout(0)` the shared work is bounded only by the deadline of the request that started it; a plain `net/http` request has none, so a hung handler then blocks every request waiting for that key. Storing the response may take up to 1s past the timeout, so slow handlers are still cached.
- **Logging:** panics, timeouts and invalid status codes are logged once per handler execution through `WithLogger`. anycache itself also logs storage write failures and failed `WithWarmUpTTL` refreshes via `slog.Default()`, so a failing background refresh appears in both.
- **Trailers and upgrades:** response trailers are not supported and are dropped. `101 Switching Protocols` responses are never stored or shared.
- **Background runs:** with warm-up, or when the triggering client disconnects, the handler may run after the original request has completed. Do not use request-scoped resources from the context (database transactions, pooled buffers, spans ended by an outer middleware) in cached handlers.
- **Fail open:** if reading from storage fails, the request is served by the handler directly and a warning is logged. If storing fails, the response is still served and shared with waiting requests. The handler never runs twice for the same request.
- **Panics:** a panicking handler returns `500`; `http.ErrAbortHandler` is re-panicked.
- **Headers:** `Age` is set on hits, hop-by-hop headers and `Set-Cookie` are never stored.
- **Invalidation:** `mw.Key(r)` returns the key for a request, to be passed to `cache.Invalidate`. Each vary-header combination has its own key.

Limitations:

- Responses are buffered in memory: `Flush` is a no-op and `Hijack` is not supported, so do not use the middleware on SSE or websocket routes. `WithMaxBodySize` only decides what is stored; larger responses are still buffered in full, so do not wrap large download or file routes.
- With the memcache backend, keep `WithMaxBodySize` comfortably below the memcache item size limit (1 MB by default); larger entries cannot be stored.
- The TTL is fixed per request (`WithTTLFunc`) and is not derived from `Cache-Control: max-age`. `ETag`/`304` and `HEAD` are not handled.
- Request headers outside the key (for example `X-Forwarded-Host`) must not affect the response. Apply response compression outside the cache middleware, or add `Accept-Encoding` to the vary headers.

## Storage backends

Backends in this repository:

- `storage/redis`
- `storage/inmemory`
- `storage/layered`
- `storage/memcache`
- `storage/badger`

### When to choose each backend

- Choose **`inmemory`** for fastest process-local caching and when restart data loss is acceptable.
- Choose **`redis`** for shared cache state across instances with standard Redis interoperability.
- Choose **`memcache`** when you need Memcached infrastructure and accept protobuf-wrapped value format + TTL limits.
- Choose **`badger`** for single-node persistent local caching without external services.
- Choose **`layered`** when you want an L1+L2 strategy (for example in-memory + Redis) and can tolerate best-effort, non-atomic cross-layer behavior.

### Memcache backend caveats

`storage/memcache` does **not** store raw user bytes directly. On every `Set`, it serializes values as protobuf `CachedItem`:

- `value` (original payload)
- `expires_at_unix` (absolute expiration timestamp)

This wrapper is required so `GetWithTTL` can reconstruct remaining TTL from stored absolute expiry.

Interoperability impact:

- Non-anycache memcached readers will see protobuf bytes, not the original raw payload.
- Non-anycache writers that store raw bytes will not match this backend's decode path.

TTL behavior implemented by this backend:

- `ttl > 30 days` is rejected.
- Positive TTL that truncates below 1 second is rejected.
- `ttl <= 0` means no expiration.

### Layered backend semantics and failures

`storage/layered` executes operations in layer order and does not provide cross-layer transactions.

`GetWithTTL` flow:

- Reads each layer from top to bottom.
- If a layer returns `ErrKeyNotExists`, it continues to the next layer.
- If a layer returns any other error, it fails immediately.
- On a lower-layer hit, it back-populates all upper layers using `Set` and the returned TTL.
- If any back-population `Set` fails, `GetWithTTL` returns that error (even though a lower layer had the value).

Write/delete flow:

- `Set` writes sequentially to each layer and returns on first error.
- `Del` deletes sequentially from each layer and returns on first error.
- Because writes/deletes are sequential, earlier layers may already be modified when a later layer fails (partial application).

Consistency expectation:

- Layer alignment is best effort over time.
- Temporary divergence between layers is possible during failures or partial updates.

## Additional examples

For runnable onboarding examples, see:

- [`anycache_example_test.go`](./anycache_example_test.go) in this repository
- pkg.go.dev examples: <https://pkg.go.dev/github.com/ksysoev/anycache>

### InMemory

```go
ctx := context.Background()

store, err := inmemory.New(10_000)
if err != nil {
	panic(err)
}
defer func() { _ = store.Close() }()

cache := anycache.New(store)
defer func() { _ = cache.Close() }()

v, err := cache.CacheS(ctx, "greeting", time.Minute, func(context.Context) (string, error) {
	return "hello", nil
})
if err != nil {
	panic(err)
}
_ = v
```

### Layered

```go
ctx := context.Background()

l1, err := inmemory.New(5_000)
if err != nil {
	panic(err)
}
defer func() { _ = l1.Close() }()

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
defer func() { _ = rdb.Close() }()
l2 := redisstorage.New(rdb)

store, err := layered.New(l1, l2)
if err != nil {
	panic(err)
}

cache := anycache.New(store)
defer func() { _ = cache.Close() }()

if _, err := cache.Cache(ctx, "user:42", 5*time.Minute, func(context.Context) ([]byte, error) {
	return []byte("value"), nil
}); err != nil {
	panic(err)
}
```

When a value is found in a lower layer, `storage/layered` back-populates upper layers for faster subsequent reads.
