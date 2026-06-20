package tests

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/require"
)

func benchmarkKey(b *testing.B, backend, scope string) string {
	b.Helper()

	return "bench:" + backend + ":" + scope + ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func BenchmarkStorageSet_AllBackends(b *testing.B) {
	ctx := context.Background()

	for _, backend := range allBackends() {
		backend := backend
		b.Run(backend.name, func(b *testing.B) {
			store, cleanup := backend.new(b)
			defer cleanup()

			keyPrefix := benchmarkKey(b, backend.name, "set")
			value := []byte("value")

			const keyspace = 128 // keep bounded to avoid backend-specific eviction behavior

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := keyPrefix + ":" + strconv.Itoa(i%keyspace)
				err := store.Set(ctx, key, value, 30*time.Second)
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkStorageGet_AllBackends(b *testing.B) {
	ctx := context.Background()

	for _, backend := range allBackends() {
		backend := backend
		b.Run(backend.name, func(b *testing.B) {
			store, cleanup := backend.new(b)
			defer cleanup()

			key := benchmarkKey(b, backend.name, "get")
			expected := []byte("value")
			require.NoError(b, store.Set(ctx, key, expected, 30*time.Second))

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				v, err := store.Get(ctx, key)
				require.NoError(b, err)
				require.Equal(b, expected, v)
			}
		})
	}
}

func BenchmarkAnyCacheHit_AllBackends(b *testing.B) {
	ctx := context.Background()

	for _, backend := range allBackends() {
		backend := backend
		b.Run(backend.name, func(b *testing.B) {
			store, cleanup := backend.new(b)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			expected := []byte("cached")
			key := benchmarkKey(b, backend.name, "cache-hit")
			require.NoError(b, store.Set(ctx, key, expected, 30*time.Second))

			generator := func(context.Context) ([]byte, error) {
				b.Fatal("generator must not be called for cache hit")
				return nil, nil
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				v, err := cache.Cache(ctx, key, generator, anycache.WithTTL(30*time.Second))
				require.NoError(b, err)
				require.Equal(b, expected, v)
			}
		})
	}
}

func BenchmarkAnyCacheMiss_AllBackends(b *testing.B) {
	ctx := context.Background()

	for _, backend := range allBackends() {
		backend := backend
		b.Run(backend.name, func(b *testing.B) {
			store, cleanup := backend.new(b)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			baseKey := benchmarkKey(b, backend.name, "cache-miss")
			generator := func(context.Context) ([]byte, error) {
				return []byte("generated"), nil
			}

			const keyspace = 128 // bounded keys, explicitly deleted to force misses

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := baseKey + ":" + strconv.Itoa(i%keyspace)
				_ = store.Del(ctx, key)

				v, err := cache.Cache(ctx, key, generator, anycache.WithTTL(30*time.Second))
				require.NoError(b, err)
				require.Equal(b, []byte("generated"), v)
			}
		})
	}
}

func BenchmarkAnyCacheBalanced80Hit20Miss_AllBackends(b *testing.B) {
	ctx := context.Background()

	for _, backend := range allBackends() {
		backend := backend
		b.Run(backend.name, func(b *testing.B) {
			store, cleanup := backend.new(b)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			expectedCached := []byte("cached")
			hitKey := benchmarkKey(b, backend.name, "cache-balanced-hit")
			require.NoError(b, store.Set(ctx, hitKey, expectedCached, 30*time.Second))

			missKey := benchmarkKey(b, backend.name, "cache-balanced-miss")
			expectedGenerated := []byte("generated")
			generator := func(context.Context) ([]byte, error) {
				return expectedGenerated, nil
			}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				key := hitKey
				expected := expectedCached
				if i%5 == 0 { // 20% misses, 80% hits
					key = missKey
					expected = expectedGenerated
					if err := store.Del(ctx, missKey); err != nil {
						b.Fatal(err)
					}
				}

				v, err := cache.Cache(ctx, key, generator, anycache.WithTTL(30*time.Second))
				require.NoError(b, err)
				require.Equal(b, expected, v)
			}
		})
	}
}
