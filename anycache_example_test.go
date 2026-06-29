package anycache_test

import (
	"context"
	"fmt"
	"os"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/ksysoev/anycache"
	badgerstorage "github.com/ksysoev/anycache/storage/badger"
	"github.com/ksysoev/anycache/storage/inmemory"
	"github.com/ksysoev/anycache/storage/layered"
	redisstorage "github.com/ksysoev/anycache/storage/redis"
	"github.com/redis/go-redis/v9"
)

func ExampleCache_Cache() {
	ctx := context.Background()

	store, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	calls := 0
	generator := func(context.Context) ([]byte, error) {
		calls++
		return []byte(fmt.Sprintf("generated-%d", calls)), nil
	}

	first, err := cache.Cache(ctx, "user:42", time.Minute, generator)
	if err != nil {
		panic(err)
	}

	second, err := cache.Cache(ctx, "user:42", time.Minute, generator)
	if err != nil {
		panic(err)
	}

	fmt.Printf("first=%s second=%s calls=%d\n", first, second, calls)
	// Output: first=generated-1 second=generated-1 calls=1
}

func ExampleCache_CacheS() {
	ctx := context.Background()

	store, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	value, err := cache.CacheS(ctx, "greeting", time.Minute, func(context.Context) (string, error) {
		return "hello", nil
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: hello
}

func ExampleCache_CacheStruct() {
	type profile struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	ctx := context.Background()

	store, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	var p profile

	err = cache.CacheStruct(ctx, "user:1", time.Minute, func(context.Context) (any, error) {
		return profile{ID: 1, Name: "Alice"}, nil
	}, &p)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%d:%s\n", p.ID, p.Name)
	// Output: 1:Alice
}

func ExampleCache_Invalidate() {
	ctx := context.Background()

	store, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	calls := 0
	generator := func(context.Context) ([]byte, error) {
		calls++
		return []byte("value"), nil
	}

	if _, err := cache.Cache(ctx, "k", time.Minute, generator); err != nil {
		panic(err)
	}

	if err := cache.Invalidate(ctx, "k"); err != nil {
		panic(err)
	}

	if _, err := cache.Cache(ctx, "k", time.Minute, generator); err != nil {
		panic(err)
	}

	fmt.Println(calls)
	// Output: 2
}

func Example_storageLayered() {
	ctx := context.Background()

	l1, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = l1.Close() }()

	l2, err := inmemory.New(100)
	if err != nil {
		panic(err)
	}
	defer func() { _ = l2.Close() }()

	store, err := layered.New(l1, l2)
	if err != nil {
		panic(err)
	}

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	value, err := cache.CacheS(ctx, "layered:key", time.Minute, func(context.Context) (string, error) {
		return "from-layered", nil
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: from-layered
}

func Example_storageRedis_setup() {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer func() { _ = rdb.Close() }()

	store := redisstorage.New(rdb)

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	fmt.Println("redis store configured")
	// Output: redis store configured
}

func Example_storageBadger() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "anycache-badger-example-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	db, err := badgerdb.Open(badgerdb.DefaultOptions(dir).WithLogger(nil))
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()

	store := badgerstorage.New(db)

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	value, err := cache.CacheS(ctx, "badger:key", time.Minute, func(context.Context) (string, error) {
		return "from-badger", nil
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(value)
	// Output: from-badger
}
