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
	memcacheStore := New(memcachedClient)

	ctx := context.Background()

	err := memcacheStore.Set(ctx, "TestMemcacheCacheStorageGetKey", []byte("testValue"), 0)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	value, err := memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey")
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	assert.Equal(t, []byte("testValue"), value, "Expected to get testValue, but got '%v'", value)

	_, err = memcacheStore.Get(ctx, "TestMemcacheCacheStorageGetKey1")

	if !errors.Is(err, anycache.ErrKeyNotExists) {
		t.Errorf("Expected to get error %v, but got '%v'", anycache.ErrKeyNotExists, err)
	}
}

func TestMemcacheCacheStorageSet(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)

	ctx := context.Background()

	err := memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey", []byte("testValue"), 0)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item, _ := memcacheStore.Get(t.Context(), "TestMemcacheCacheStorageSetKey")

	if string(item) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item)
	}

	err = memcacheStore.Set(ctx, "TestMemcacheCacheStorageSetKey1", []byte("testValue"), 2*time.Second)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	item1, _ := memcacheStore.Get(t.Context(), "TestMemcacheCacheStorageSetKey1")

	if string(item1) != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", item1)
	}
}

func TestMemcacheCacheStorageTTL(t *testing.T) {
	memcachedClient := memcache.New(getMemcachedHost())
	memcacheStore := New(memcachedClient)

	ctx := context.Background()

	err := memcacheStore.Set(ctx, "TestMemcacheCacheStorageTTLKey", []byte("testValue"), 10)
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	hasTTL, ttl, err := memcacheStore.TTL(ctx, "TestMemcacheCacheStorageTTLKey")
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it does not have")
	}

	if ttl.Seconds() > 0 {
		t.Errorf("Current implementation of memcache does not support meta commands to get TTL, so it should always return 0, but we got %v", ttl.Milliseconds())
	}

	_, _, err = memcacheStore.TTL(ctx, "TestMemcacheCacheStorageTTLKey1")

	if !errors.Is(err, anycache.ErrKeyNotExists) {
		t.Errorf("Expected to get error %v, but got '%v'", anycache.ErrKeyNotExists, err)
	}

	err = memcacheStore.Set(ctx, "TestMemcacheCacheStorageTTLKey2", []byte("testValue"), 0)
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
	memcacheStore := New(memcachedClient)

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
