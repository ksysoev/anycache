package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/ksysoev/anycache"
	"github.com/ksysoev/anycache/storage/badger"
	"github.com/ksysoev/anycache/storage/inmemory"
	"github.com/ksysoev/anycache/storage/layered"
	memcachestore "github.com/ksysoev/anycache/storage/memcache"
	redisstore "github.com/ksysoev/anycache/storage/redis"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backendFactory struct {
	new  func(tb testing.TB) (anycache.CacheStorage, func())
	name string
}

func allBackends() []backendFactory {
	return []backendFactory{
		{
			name: "inmemory",
			new: func(tb testing.TB) (anycache.CacheStorage, func()) {
				tb.Helper()

				s, err := inmemory.New(256)
				require.NoError(tb, err)

				return s, func() { _ = s.Close() }
			},
		},
		{
			name: "badger",
			new: func(tb testing.TB) (anycache.CacheStorage, func()) {
				tb.Helper()

				db, err := badgerdb.Open(badgerdb.DefaultOptions(tb.TempDir()).WithLogger(nil))
				require.NoError(tb, err)

				return badger.New(db), func() { require.NoError(tb, db.Close()) }
			},
		},
		{
			name: "redis",
			new: func(tb testing.TB) (anycache.CacheStorage, func()) {
				tb.Helper()

				client := goredis.NewClient(redisOptions())

				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()

				if err := client.Ping(ctx).Err(); err != nil {
					_ = client.Close()

					tb.Skipf("skipping redis backend, unavailable at %s: %v", redisOptions().Addr, err)
				}

				return redisstore.New(client), func() { _ = client.Close() }
			},
		},
		{
			name: "memcache",
			new: func(tb testing.TB) (anycache.CacheStorage, func()) {
				tb.Helper()

				client := memcache.New(memcacheAddr())
				if err := client.Ping(); err != nil {
					tb.Skipf("skipping memcache backend, unavailable at %s: %v", memcacheAddr(), err)
				}

				return memcachestore.New(client), func() {}
			},
		},
		{
			name: "layered",
			new: func(tb testing.TB) (anycache.CacheStorage, func()) {
				tb.Helper()

				s1, err := inmemory.New(256)
				require.NoError(tb, err)

				s2, err := inmemory.New(256)
				require.NoError(tb, err)

				s, err := layered.New(s1, s2)
				require.NoError(tb, err)

				return s, func() {
					_ = s1.Close()
					_ = s2.Close()
				}
			},
		},
	}
}

func redisOptions() *goredis.Options {
	host := os.Getenv("TEST_REDIS_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("TEST_REDIS_PORT")
	if port == "" {
		port = "6379"
	}

	return &goredis.Options{Addr: fmt.Sprintf("%s:%s", host, port)}
}

func memcacheAddr() string {
	host := os.Getenv("TEST_MEMCACHED_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("TEST_MEMCACHED_PORT")
	if port == "" {
		port = "11211"
	}

	return fmt.Sprintf("%s:%s", host, port)
}

func uniqueKey(tb testing.TB, backend string) string {
	tb.Helper()

	testName := strings.NewReplacer("/", "_", " ", "_").Replace(tb.Name())

	return fmt.Sprintf("it:%s:%s:%d", backend, testName, time.Now().UnixNano())
}

func TestIntegration_CacheMissThenHit_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			var calls atomic.Int32

			generator := func(context.Context) ([]byte, error) {
				n := calls.Add(1)
				return []byte(fmt.Sprintf("value-%d", n)), nil
			}

			key := uniqueKey(t, backend.name)

			v1, err := cache.Cache(t.Context(), key, generator, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, []byte("value-1"), v1)

			v2, err := cache.Cache(t.Context(), key, generator, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, []byte("value-1"), v2)
			assert.EqualValues(t, 1, calls.Load(), "generator must be called only once for a cache hit")
		})
	}
}

func TestIntegration_CacheS_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			var calls atomic.Int32

			generator := func(context.Context) (string, error) {
				calls.Add(1)
				return "value", nil
			}

			key := uniqueKey(t, backend.name)

			v1, err := cache.CacheS(t.Context(), key, generator, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, "value", v1)

			v2, err := cache.CacheS(t.Context(), key, generator, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, "value", v2)
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestIntegration_CacheStruct_AllBackends(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			var calls atomic.Int32

			generator := func(context.Context) (any, error) {
				n := calls.Add(1)
				return payload{Name: "john", Count: int(n)}, nil
			}

			key := uniqueKey(t, backend.name)

			var p1 payload

			err := cache.CacheStruct(t.Context(), key, generator, &p1, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, payload{Name: "john", Count: 1}, p1)

			var p2 payload

			err = cache.CacheStruct(t.Context(), key, generator, &p2, anycache.WithTTL(5*time.Second))
			require.NoError(t, err)
			assert.Equal(t, payload{Name: "john", Count: 1}, p2)
			assert.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestIntegration_Invalidate_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			key := uniqueKey(t, backend.name)

			v1, err := cache.Cache(t.Context(), key, func(context.Context) ([]byte, error) {
				return []byte("v1"), nil
			}, anycache.WithTTL(30*time.Second))
			require.NoError(t, err)
			assert.Equal(t, []byte("v1"), v1)

			require.NoError(t, cache.Invalidate(t.Context(), key))

			v2, err := cache.Cache(t.Context(), key, func(context.Context) ([]byte, error) {
				return []byte("v2"), nil
			}, anycache.WithTTL(30*time.Second))
			require.NoError(t, err)
			assert.Equal(t, []byte("v2"), v2)
		})
	}
}

func TestIntegration_KeyPrefix_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			prefix := uniqueKey(t, backend.name) + ":prefix:"

			cache := anycache.New(store, anycache.WithKeyPrefix(prefix))
			defer func() { _ = cache.Close() }()

			key := "my-key"
			v, err := cache.Cache(t.Context(), key, func(context.Context) ([]byte, error) {
				return []byte("prefixed-value"), nil
			}, anycache.WithTTL(10*time.Second))
			require.NoError(t, err)
			assert.Equal(t, []byte("prefixed-value"), v)

			prefixedVal, err := store.Get(t.Context(), prefix+key)
			require.NoError(t, err)
			assert.Equal(t, []byte("prefixed-value"), prefixedVal)

			_, err = store.Get(t.Context(), key)
			assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
		})
	}
}

func TestIntegration_WarmUpTTL_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			key := uniqueKey(t, backend.name)
			require.NoError(t, store.Set(t.Context(), key, []byte("stale"), 3*time.Second))

			var calls atomic.Int32

			generator := func(context.Context) ([]byte, error) {
				calls.Add(1)
				return []byte("fresh"), nil
			}

			v, err := cache.Cache(
				t.Context(),
				key,
				generator,
				anycache.WithTTL(10*time.Second),
				anycache.WithWarmUpTTL(4*time.Second),
			)
			require.NoError(t, err)
			assert.Equal(t, []byte("stale"), v, "warm-up should return current value immediately")

			assert.Eventually(t, func() bool {
				stored, getErr := store.Get(t.Context(), key)
				if getErr != nil {
					return false
				}

				return string(stored) == "fresh"
			}, 2*time.Second, 100*time.Millisecond)

			assert.EqualValues(t, 1, calls.Load(), "warm-up should run generator only once")
		})
	}
}

func TestIntegration_Singleflight_AllBackends(t *testing.T) {
	for _, backend := range allBackends() {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store, cleanup := backend.new(t)
			defer cleanup()

			cache := anycache.New(store)
			defer func() { _ = cache.Close() }()

			key := uniqueKey(t, backend.name)

			const workers = 12

			var calls atomic.Int32

			//nolint:unparam // signature requires error, but we don't need it in this test
			generator := func(context.Context) ([]byte, error) {
				calls.Add(1)
				time.Sleep(80 * time.Millisecond)

				return []byte("shared"), nil
			}

			results := make([][]byte, workers)
			errCh := make(chan error, workers)

			var wg sync.WaitGroup
			wg.Add(workers)

			for i := range workers {
				i := i

				go func() {
					defer wg.Done()

					v, err := cache.Cache(t.Context(), key, generator, anycache.WithTTL(10*time.Second))
					if err != nil {
						errCh <- err
						return
					}

					results[i] = v
				}()
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				require.NoError(t, err)
			}

			for i := 0; i < workers; i++ {
				assert.Equal(t, []byte("shared"), results[i])
			}

			assert.EqualValues(t, 1, calls.Load(), "singleflight must deduplicate concurrent generation")
		})
	}
}
