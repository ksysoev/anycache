# AnyCache

[![tests](https://github.com/ksysoev/anycache/actions/workflows/main.yml/badge.svg)](https://github.com/ksysoev/anycache/actions/workflows/main.yml)
[![codecov](https://codecov.io/gh/ksysoev/anycache/branch/main/graph/badge.svg?token=J7936BN4R2)](https://codecov.io/gh/ksysoev/anycache)

anycache is a Go library that provides lazy caching with the possibility to use different cache storages. It allows you to cache the results of expensive operations and retrieve them quickly on subsequent requests.

# Features

- Lazy caching: cache values only when they are requested, not when they are generated.
- Multiple cache storages: use Redis, Memcached, or any other cache storage that implements the CacheStorage interface.
- Cache warming up: pre-populate the cache with values before they got expired.
- Randomized TTL: add a random factor to the time-to-live (TTL) of cached values to avoid cache stampedes.
- Serilization: cache JSON-encoded values and decode them automatically on retrieval.

# Installation

To use anycache in your Go project, you can install it using go get:

```
go get github.com/ksysoev/anycache
```

# Usage

Here's an example of how to use anycache to cache the result of a function that generates a random number:

```golang
package main

import (
    "fmt"
    "math/rand"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/ksysoev/anycache"
    "github.com/ksysoev/anycache/storage/redis"
)

func main() {
    redisClient := redis.NewClient(redis.Options{
        Addr: "localhost:6379",
    })

    redisStorage := redisstor.NewRedisCacheStorage(redisClient)

    // Creates anycache with 10% TTL randomization
    cache := anycache.NewCache(redisStorage, WithTTLRandomization(10))
    defer cache.Close()

    generator := func() (string, error) {
        randomNumber := rand.Intn(100)
        return fmt.Sprintf("%d", randomNumber), nil
    }

    value, err := cache.Cache(
        "random_number_key", 
        generator, 
        WithTTL(5 * time.Minute), 
        WithWarmUpTTL(1 * time.Minute)
    )

    if err != nil {
        fmt.Printf("Error caching value: %v\n", err)
        return
    }

    fmt.Printf("Cached value: %s\n", value)
}
```

In this example, we create a Redis cache storage instance using the redis package, and we create a new cache instance using NewCache with the Redis cache storage and some cache options.

We define a cache key, a time-to-live (TTL) duration, a warm-up TTL duration, and a generator function that generates a random number. We also define some cache item options that include the TTL and the warm-up TTL.

We use the Cache method of the cache instance to cache the result of the generator function with the given key and options. If the value is not already cached, the generator function is called to generate the value, and the value is stored in the cache storage with the given key and options. If the value is already cached, it is retrieved from the cache storage.

We print the cached value to the console.
