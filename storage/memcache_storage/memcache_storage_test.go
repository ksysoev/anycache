package memcache_storage

import (
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

func TestRedisCacheStorageGet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	memcachedClient.Set(&memcache.Item{Key: "testKey", Value: []byte("testValue")})

	value, err := memcacheStore.Get("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if value != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", value)
	}

	_, err = memcacheStore.Get("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	memcachedClient.DeleteAll()
}

func TestRedisCacheStorageSet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	err := memcacheStore.Set("testKey", "testValue", storage.CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item, _ := memcachedClient.Get("testKey")

	if string(item.Value) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item.Value)
	}

	err = memcacheStore.Set("testKey1", "testValue", storage.CacheStorageItemOptions{TTL: 2 * time.Second})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item1, _ := memcachedClient.Get("testKey1")

	if string(item1.Value) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item1.Value)
	}

	if item1.Expiration <= 0 || item1.Expiration > 2 {
		t.Errorf("Expected to get valid TTL, but it has value %v", item1.Expiration)
	}

	memcachedClient.DeleteAll()
}

func TestRedisCacheStorageTTL(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	memcachedClient.Set(&memcache.Item{Key: "testKey", Value: []byte("testValue"), Expiration: 1})

	hasTTL, ttl, err := memcacheStore.TTL("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Milliseconds() < 0 || ttl.Milliseconds() > 1000 {
		t.Errorf("Expected to get TTL as 1000 millisecond, but it has value %v microseconds", ttl.Milliseconds())
	}

	_, _, err = memcacheStore.TTL("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	memcachedClient.Set(&memcache.Item{Key: "testKey2", Value: []byte("testValue")})
	hasTTL, _, err = memcacheStore.TTL("testKey2")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it has")
	}

	memcachedClient.DeleteAll()
}

func TestRedisCacheStorageDel(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := NewMemcachedCacheStorage(memcachedClient)

	memcachedClient.Set(&memcache.Item{Key: "testKey2", Value: []byte("testValue")})

	memcacheStore.Del("testKey")

	_, err := memcachedClient.Get("testKey")

	if !errors.Is(err, memcache.ErrCacheMiss) {
		t.Errorf("Expected to get error %v, but got '%v'", memcache.ErrCacheMiss, err)
	}

	memcachedClient.DeleteAll()
}
