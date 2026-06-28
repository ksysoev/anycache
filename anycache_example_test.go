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

	first, _ := cache.Cache(ctx, "user:42", time.Minute, generator)
	second, _ := cache.Cache(ctx, "user:42", time.Minute, generator)

	fmt.Printf("first=%s second=%s calls=%d\n", first, second, calls)
	// Output: first=generated-1 second=generated-1 calls=1
}

func ExampleCache_CacheS() {
	ctx := context.Background()

	store, _ := inmemory.New(100)
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	value, _ := cache.CacheS(ctx, "greeting", time.Minute, func(context.Context) (string, error) {
		return "hello", nil
	})

	fmt.Println(value)
	// Output: hello
}

func ExampleCache_CacheStruct() {
	type profile struct {
		Name string `json:"name"`
		ID   int    `json:"id"`
	}

	ctx := context.Background()

	store, _ := inmemory.New(100)
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	var p profile

	_ = cache.CacheStruct(ctx, "user:1", time.Minute, func(context.Context) (any, error) {
		return profile{ID: 1, Name: "Alice"}, nil
	}, &p)

	fmt.Printf("%d:%s\n", p.ID, p.Name)
	// Output: 1:Alice
}

func ExampleCache_Invalidate() {
	ctx := context.Background()

	store, _ := inmemory.New(100)
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	calls := 0
	generator := func(context.Context) ([]byte, error) {
		calls++
		return []byte("value"), nil
	}

	_, _ = cache.Cache(ctx, "k", time.Minute, generator)
	_ = cache.Invalidate(ctx, "k")
	_, _ = cache.Cache(ctx, "k", time.Minute, generator)

	fmt.Println(calls)
	// Output: 2
}

func Example_storageLayered() {
	ctx := context.Background()

	l1, _ := inmemory.New(100)
	defer func() { _ = l1.Close() }()

	l2, _ := inmemory.New(100)
	defer func() { _ = l2.Close() }()

	store, _ := layered.New(l1, l2)

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	value, _ := cache.CacheS(ctx, "layered:key", time.Minute, func(context.Context) (string, error) {
		return "from-layered", nil
	})

	fmt.Println(value)
	// Output: from-layered
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

	value, _ := cache.CacheS(ctx, "badger:key", time.Minute, func(context.Context) (string, error) {
		return "from-badger", nil
	})

	fmt.Println(value)
	// Output: from-badger
}
