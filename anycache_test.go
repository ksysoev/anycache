package anycache

import (
	"errors"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/ksysoev/anycache/storage"
	"github.com/ksysoev/anycache/storage/redis_storage"
)

// Map storage tests
func TestCache(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage[string, string]()
	cache := NewCache[string, string](cacheStore)

	val, err := cache.Cache("testKey", func() (string, error) { return "testValue", nil }, CacheItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache("testKey", func() (string, error) { return "testValue1", nil }, CacheItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache("testKey1", func() (string, error) { return "", errors.New("TestError") }, CacheItemOptions{})

	if err == errors.New("TestError") {
		t.Errorf("Expected to get TestError, but got %v", err)
	}

	if val != "" {
		t.Errorf("Expected to get empty string, but got '%v'", val)
	}
}

func TestCacheConcurrency(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage[string, string]()
	cache := NewCache[string, string](cacheStore)

	results := make(chan string)

	go func(c *Cache[string, string], ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue", nil
		}, CacheItemOptions{})
		ch <- val
	}(&cache, results)

	go func(c *Cache[string, string], ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue1", nil
		}, CacheItemOptions{})
		ch <- val
	}(&cache, results)

	val1, val2 := <-results, <-results

	if val1 != "testValue" && val1 != "testValue1" {
		t.Errorf("Expected to get testValue as a result, but got '%v'", val1)
	}

	if val1 != val2 {
		t.Errorf("Expected to get same result for concurent requests, but got '%v' and '%v", val1, val2)
	}
}

func TestCacheWarmingUp(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage[string, string]()
	cache := NewCache[string, string](cacheStore)
	cacheOptions := CacheItemOptions{TTL: 2 * time.Millisecond, WarmUpTTL: time.Millisecond}

	val, _ := cache.Cache("testKey", func() (string, error) {
		return "testValue", nil
	}, cacheOptions)

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	results := make(chan string)

	time.Sleep(time.Millisecond)

	go func(c *Cache[string, string], ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "newTestValue", nil
		}, cacheOptions)
		ch <- val
	}(&cache, results)

	go func(c *Cache[string, string], ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "newTestValue", nil
		}, cacheOptions)
		ch <- val
	}(&cache, results)

	val1 := <-results
	val2 := <-results

	// First request
	if val1 != "testValue" {
		t.Errorf("Expected to get testValue as a result, but got '%v'", val1)
	}

	if val2 != "newTestValue" {
		t.Errorf("Expected to get new value, but got '%v'", val2)
	}
}

// Redis storage tests
func TestCacheRedisStorage(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache[string, string](cacheStore)

	mock.ExpectGet("testKey").RedisNil()
	mock.ExpectGet("testKey").RedisNil()
	mock.ExpectSet("testKey", "testValue", 0).SetVal("OK")

	val, err := cache.Cache("testKey", func() (string, error) { return "testValue", nil }, CacheItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

	mock.ExpectGet("testKey").SetVal("testValue")
	val, err = cache.Cache("testKey", func() (string, error) { return "testValue1", nil }, CacheItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

	mock.ExpectGet("testKey1").RedisNil()
	mock.ExpectGet("testKey1").RedisNil()
	val, err = cache.Cache("testKey1", func() (string, error) { return "", errors.New("TestError") }, CacheItemOptions{})

	if err == errors.New("TestError") {
		t.Errorf("Expected to get TestError, but got %v", err)
	}

	if val != "" {
		t.Errorf("Expected to get empty string, but got '%v'", val)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
