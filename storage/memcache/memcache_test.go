package memcachestor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache/storage"
)

func getMemcachedHost() string {
	TestRedisHost := os.Getenv("TEST_MEMCACHED_HOST")
	if TestRedisHost == "" {
		TestRedisHost = "localhost"
	}

	TestRedisPort := os.Getenv("TEST_MEMCACHED_PORT")
	if TestRedisPort == "" {
		TestRedisPort = "11211"
	}

	return fmt.Sprintf("%s:%s", TestRedisHost, TestRedisPort)
}

func TestMemcacheCacheStorageGet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	ctx := context.Background()

	err := memcachedClient.Set(&memcache.Item{Key: "TestMemcacheCacheStorageGetKey", Value: []byte("testValue")})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	value, err := memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if value != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", value)
	}

	_, err = memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}
}

func TestMemcacheCacheStorageSet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	ctx := context.Background()

	err := memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey", "testValue", storage.CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item, _ := memcachedClient.Get("TestMemcacheCacheStorageSetKey")

	if string(item.Value) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item.Value)
	}

	err = memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey1", "testValue", storage.CacheStorageItemOptions{TTL: 2 * time.Second})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item1, _ := memcachedClient.Get("TestMemcacheCacheStorageSetKey1")

	if string(item1.Value) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item1.Value)
	}
}

func TestMemcacheCacheStorageTTL(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	ctx := context.Background()

	err := memcachedClient.Set(&memcache.Item{Key: "TestMemcacheCacheStorageTTLKey", Value: []byte("testValue"), Expiration: 1})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	hasTTL, ttl, err := memcacheStore.TTL(ctx, "TestMemcacheCacheStorageTTLKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Current implementation of memcache does not support meta commands to get TTL, so it should always return false")
	}

	if ttl.Milliseconds() != 0 {
		t.Errorf("Current implementation of memcache does not support meta commands to get TTL, so it should always return 0, but we got %v", ttl.Milliseconds())
	}

	_, _, err = memcacheStore.TTL(ctx, "TestMemcacheCacheStorageTTLKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	err = memcachedClient.Set(&memcache.Item{Key: "TestMemcacheCacheStorageTTLKey2", Value: []byte("testValue")})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	hasTTL, _, err = memcacheStore.TTL(ctx, "TestMemcacheCacheStorageTTLKey2")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it has")
	}
}

func TestMemcacheCacheStorageDel(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	ctx := context.Background()

	err := memcachedClient.Set(&memcache.Item{Key: "TestMemcacheCacheStorageDelKey", Value: []byte("testValue")})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	_, err = memcacheStore.Del(ctx, "TestMemcacheCacheStorageDelKey")
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	_, err = memcachedClient.Get("TestMemcacheCacheStorageDelKey")
	if !errors.Is(err, memcache.ErrCacheMiss) {
		t.Errorf("Expected to get error %v, but got '%v'", memcache.ErrCacheMiss, err)
	}
}
