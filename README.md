# anycache

`anycache` is a lazy caching library for Go with pluggable storage backends. It helps reduce repeated expensive work, supports cache stampede mitigation, and can warm up entries before expiration.

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
	cache := anycache.New(redisstorage.New(rdb))
	defer cache.Close()

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

// CacheStruct
type Profile struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

var p Profile
err = cache.CacheStruct(ctx, "user:profile", 5*time.Minute, func(context.Context) (any, error) {
	return Profile{ID: 42, Name: "Alice"}, nil
}, &p)

// Invalidate
err = cache.Invalidate(ctx, "user:profile")
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

- Concurrent same-key requests are deduplicated (singleflight-style).
- `WithWarmUpTTL` checks remaining TTL, returns the current cached value immediately, then refreshes asynchronously.
- `WithTimeout` runs internal storage/generator work on the cache base context (`WithBaseContext` or default), so caller context values/cancellation are not directly propagated into internal work.
- `Close()` should be called to cancel background work and wait for warm-up goroutines to finish.

## Storage backends

Included backends in this repository:

- `storage/redis`
- `storage/inmemory`
- `storage/layered`

Additional implementations: `storage/memcache`, `storage/badger`.

## Additional examples

### InMemory

```go
store, err := inmemory.New(10_000)
if err != nil {
	panic(err)
}
defer store.Close()

cache := anycache.New(store)
defer cache.Close()

v, err := cache.CacheS(ctx, "greeting", time.Minute, func(context.Context) (string, error) {
	return "hello", nil
})
_ = v
```

### Layered

```go
l1, err := inmemory.New(5_000)
if err != nil {
	panic(err)
}

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
l2 := redisstorage.New(rdb)

store, err := layered.New(l1, l2)
if err != nil {
	panic(err)
}

cache := anycache.New(store)
defer cache.Close()

_, err = cache.Cache(ctx, "user:42", 5*time.Minute, func(context.Context) ([]byte, error) {
	return []byte("value"), nil
})
```

When a value is found in a lower layer, `storage/layered` back-populates upper layers for faster subsequent reads.
