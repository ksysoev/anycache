package memcache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/assert"
)

func getMemcachedHost() string {
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

<<<<<<< Updated upstream
func TestMemcacheCacheStorageGet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)
=======
// newStore is a helper that creates a store and client pair wired to the test server.
func newStore(t *testing.T) (*MemcachedCacheStorage, *memcache.Client) {
	t.Helper()
>>>>>>> Stashed changes

	client := memcache.New(getMemcachedHost())
	return NewMemcachedCacheStorage(client), client
}

// TestGet verifies that a value written through the store can be read back.
func TestGet(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	const key = "TestGet_key"

	if err := store.Set(ctx, key, "hello", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got != "hello" {
		t.Errorf("Get = %q, want %q", got, "hello")
	}
}

// TestGetMiss verifies that reading a nonexistent key returns ErrKeyNotExists.
func TestGetMiss(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

<<<<<<< Updated upstream
	err := memcachedClient.Set(&memcache.Item{Key: "TestMemcacheCacheStorageGetKey", Value: []byte("testValue")})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	value, err := memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey")
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	assert.Equal(t, []byte("testValue"), value, "Expected to get testValue, but got '%v'", value)

	_, err = memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey1")

=======
	_, err := store.Get(ctx, "TestGetMiss_nonexistent")
>>>>>>> Stashed changes
	if !errors.Is(err, anycache.ErrKeyNotExists) {
		t.Errorf("Get missing key: got err %v, want ErrKeyNotExists", err)
	}
}

<<<<<<< Updated upstream
func TestMemcacheCacheStorageSet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)

	ctx := context.Background()

	err := memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey", []byte("testValue"), 0)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}
=======
// TestSet verifies that Set round-trips through Get correctly,
// with and without TTL.
func TestSet(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	t.Run("no TTL", func(t *testing.T) {
		const key = "TestSet_noTTL"
>>>>>>> Stashed changes

		if err := store.Set(ctx, key, "value1", 0); err != nil {
			t.Fatalf("Set: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

<<<<<<< Updated upstream
	err = memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey1", []byte("testValue"), 2*time.Second)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}
=======
		if got != "value1" {
			t.Errorf("Get = %q, want %q", got, "value1")
		}
	})
>>>>>>> Stashed changes

	t.Run("with TTL", func(t *testing.T) {
		const key = "TestSet_withTTL"

		if err := store.Set(ctx, key, "value2", 10*time.Second); err != nil {
			t.Fatalf("Set: %v", err)
		}

		got, err := store.Get(ctx, key)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if got != "value2" {
			t.Errorf("Get = %q, want %q", got, "value2")
		}
	})
}

<<<<<<< Updated upstream
func TestMemcacheCacheStorageTTL(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)

=======
// TestTTL verifies TTL retrieval across three scenarios:
//  1. Item stored with a TTL  → (true,  remaining > 0, nil)
//  2. Item stored without TTL → (false, 0,             nil)
//  3. Key does not exist      → (false, 0,             ErrKeyNotExists)
func TestTTL(t *testing.T) {
	store, _ := newStore(t)
>>>>>>> Stashed changes
	ctx := context.Background()

	t.Run("with TTL", func(t *testing.T) {
		const key = "TestTTL_withTTL"
		const ttl = 30 * time.Second

		if err := store.Set(ctx, key, "v", ttl); err != nil {
			t.Fatalf("Set: %v", err)
		}

		hasTTL, remaining, err := store.TTL(ctx, key)
		if err != nil {
			t.Fatalf("TTL: %v", err)
		}

		if !hasTTL {
			t.Error("TTL hasTTL = false, want true")
		}

		// Allow a small delta for test execution time.
		if remaining <= 0 || remaining > ttl {
			t.Errorf("TTL remaining = %v, want 0 < remaining <= %v", remaining, ttl)
		}
	})

	t.Run("no TTL", func(t *testing.T) {
		const key = "TestTTL_noTTL"

		if err := store.Set(ctx, key, "v", 0); err != nil {
			t.Fatalf("Set: %v", err)
		}

		hasTTL, remaining, err := store.TTL(ctx, key)
		if err != nil {
			t.Fatalf("TTL: %v", err)
		}

		if hasTTL {
			t.Error("TTL hasTTL = true, want false")
		}

		if remaining != 0 {
			t.Errorf("TTL remaining = %v, want 0", remaining)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, _, err := store.TTL(ctx, "TestTTL_nonexistent")
		if !errors.Is(err, anycache.ErrKeyNotExists) {
			t.Errorf("TTL missing key: got err %v, want ErrKeyNotExists", err)
		}
	})
}

<<<<<<< Updated upstream
func TestMemcacheCacheStorageDel(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)

=======
// TestGetWithTTL verifies that GetWithTTL returns both value and remaining TTL.
func TestGetWithTTL(t *testing.T) {
	store, _ := newStore(t)
>>>>>>> Stashed changes
	ctx := context.Background()

	t.Run("with TTL", func(t *testing.T) {
		const key = "TestGetWithTTL_withTTL"
		const ttl = 30 * time.Second

		if err := store.Set(ctx, key, "hello", ttl); err != nil {
			t.Fatalf("Set: %v", err)
		}

		value, remaining, err := store.GetWithTTL(ctx, key)
		if err != nil {
			t.Fatalf("GetWithTTL: %v", err)
		}

		if value != "hello" {
			t.Errorf("value = %q, want %q", value, "hello")
		}

		if remaining <= 0 || remaining > ttl {
			t.Errorf("remaining = %v, want 0 < remaining <= %v", remaining, ttl)
		}
	})

	t.Run("no TTL", func(t *testing.T) {
		const key = "TestGetWithTTL_noTTL"

		if err := store.Set(ctx, key, "hello", 0); err != nil {
			t.Fatalf("Set: %v", err)
		}

		value, remaining, err := store.GetWithTTL(ctx, key)
		if err != nil {
			t.Fatalf("GetWithTTL: %v", err)
		}

		if value != "hello" {
			t.Errorf("value = %q, want %q", value, "hello")
		}

		if remaining != 0 {
			t.Errorf("remaining = %v, want 0", remaining)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		_, _, err := store.GetWithTTL(ctx, "TestGetWithTTL_nonexistent")
		if !errors.Is(err, anycache.ErrKeyNotExists) {
			t.Errorf("GetWithTTL missing key: got err %v, want ErrKeyNotExists", err)
		}
	})
}

// TestDel verifies that Del removes a key.
func TestDel(t *testing.T) {
	store, client := newStore(t)
	ctx := context.Background()
	const key = "TestDel_key"

	if err := store.Set(ctx, key, "to-be-deleted", 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	ok, err := store.Del(ctx, key)
	if err != nil {
		t.Fatalf("Del: %v", err)
	}

	if !ok {
		t.Error("Del returned false, want true")
	}

	// Confirm deletion via the raw client.
	_, err = client.Get(key)
	if !errors.Is(err, memcache.ErrCacheMiss) {
		t.Errorf("After Del, raw Get err = %v, want ErrCacheMiss", err)
	}
}

// TestDelMissing verifies that Del on a nonexistent key returns ErrKeyNotExists.
func TestDelMissing(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	_, err := store.Del(ctx, "TestDelMissing_nonexistent")
	if !errors.Is(err, anycache.ErrKeyNotExists) {
		t.Errorf("Del missing key: got err %v, want ErrKeyNotExists", err)
	}
}
