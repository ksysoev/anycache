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

### Request-level options

- `WithWarmUpTTL(d)` — if remaining TTL is below `d`, serve current value and refresh in background.
- `WithMetric(hook)` — override metric hook for one call.
- `WithTimeout(d)` — timeout for internal storage + generation work.

Metric states: `hit`, `miss`, `warm_up`, `error`.

## Important semantics

- **Singleflight dedupe scope:** concurrent requests for the same key are deduplicated within a single `anycache.Cache` instance.
- **Warm-up behavior (`WithWarmUpTTL`):** when a key exists and its remaining TTL is `> 0` and `<= warmUpTTL`, anycache returns the current cached value immediately and schedules a background refresh.
- **Warm-up lock semantics:** only one warm-up refresh per key is started at a time; concurrent requests do not start duplicate warm-up goroutines.
- **Timeout and base context (`WithTimeout` + `WithBaseContext`):** internal storage and generator work runs on the cache base context (default or `WithBaseContext`). With `WithTimeout`, a timeout is applied to that base context for internal work.
- **Caller cancellation expectations:** because internal work uses the cache base context, caller context values/cancellation are not directly propagated into internal storage/generator execution.
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

## Storage backends

Backends in this repository:

- `storage/redis`
- `storage/inmemory`
- `storage/layered`
- `storage/memcache`
- `storage/badger`

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
