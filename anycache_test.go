package anycache

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache/storage/memcache_storage"
	"github.com/ksysoev/anycache/storage/redis_storage"
	"github.com/redis/go-redis/v9"
)

func getRedisOptions() *redis.Options {
	TestRedisHost := os.Getenv("TEST_REDIS_HOST")
	if TestRedisHost == "" {
		TestRedisHost = "localhost"
	}

	TestRedisPort := os.Getenv("TEST_REDIS_PORT")
	if TestRedisPort == "" {
		TestRedisPort = "6379"
	}

	return &redis.Options{Addr: fmt.Sprintf("%s:%s", TestRedisHost, TestRedisPort)}
}

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

func getCacheStorages() map[string]CacheStorage {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := redis_storage.NewRedisCacheStorage(redisClient)

	memcachedClient := memcache.New(getMemcachedHost())
	memcachedStore := memcache_storage.NewMemcachedCacheStorage(memcachedClient)

	return map[string]CacheStorage{
		"redis":     redisStore,
		"memcached": memcachedStore,
	}
}

func TestCache(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage, CacheOptions{})

		val, err := cache.Cache("TestCacheKey", func() (string, error) { return "testValue", nil }, CacheItemOptions{})

		if err != nil {
			t.Errorf("%v: Expected to get no error, but got %v", storageName, err)
		}

		if val != "testValue" {
			t.Errorf("%v: Expected to get testValue, but got '%v'", storageName, val)
		}

		val, err = cache.Cache("TestCacheKey", func() (string, error) { return "testValue1", nil }, CacheItemOptions{})

		if err != nil {
			t.Errorf("%v: Expected to get no error, but got %v", storageName, err)
		}

		if val != "testValue" {
			t.Errorf("%v: Expected to get testValue, but got '%v'", storageName, val)
		}

		val, err = cache.Cache("TestCacheKey1", func() (string, error) { return "", errors.New("TestError") }, CacheItemOptions{})

		if err == errors.New("TestError") {
			t.Errorf("%v: Expected to get TestError, but got %v", storageName, err)
		}

		if val != "" {
			t.Errorf("%v: Expected to get empty string, but got '%v'", storageName, val)
		}
	}
}

func TestCacheConcurrency(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage, CacheOptions{})

		results := make(chan string)

		go func(c *Cache, ch chan string) {
			val, _ := c.Cache("TestCacheConcurrencyKey", func() (string, error) {
				time.Sleep(time.Millisecond)
				return "testValue", nil
			}, CacheItemOptions{})
			ch <- val
		}(&cache, results)

		go func(c *Cache, ch chan string) {
			val, _ := c.Cache("TestCacheConcurrencyKey", func() (string, error) {
				time.Sleep(time.Millisecond)
				return "testValue1", nil
			}, CacheItemOptions{})
			ch <- val
		}(&cache, results)

		val1, val2 := <-results, <-results

		if val1 != "testValue" && val1 != "testValue1" {
			t.Errorf("%v: Expected to get testValue as a result, but got '%v'", storageName, val1)
		}

		if val1 != val2 {
			t.Errorf("%v: Expected to get same result for concurent requests, but got '%v' and '%v", storageName, val1, val2)
		}
	}
}

func TestCacheWarmingUp(t *testing.T) {
	// For now, we test only Redis storage becuase Memcache client does not support TTL
	redisClient := redis.NewClient(getRedisOptions())
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore, CacheOptions{})
	cacheOptions := CacheItemOptions{TTL: 2 * time.Second, WarmUpTTL: 1 * time.Second}

	val, err := cache.Cache("TestCacheWarmingUpKey", func() (string, error) {
		return "testValue", nil
	}, cacheOptions)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	results := make(chan string)

	time.Sleep(time.Millisecond * 1001)

	go func(c *Cache, ch chan string) {
		val, err := c.Cache("TestCacheWarmingUpKey", func() (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, cacheOptions)

		if err != nil {
			t.Errorf("Expected to get no error, but got %v", err)
		}
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
		val, err := c.Cache("TestCacheWarmingUpKey", func() (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, cacheOptions)

		if err != nil {
			t.Errorf("Expected to get no error, but got %v", err)
		}

		ch <- val
	}(&cache, results)

	val1 := <-results
	val2 := <-results

	// First request
	if !(val1 == "testValue" || val2 == "testValue") {
		t.Errorf("Expected to get at least one testValue as a result, but got '%v' and %v", val1, val2)
	}

	if !(val1 == "newTestValue" || val2 == "newTestValue") {
		t.Errorf("Expected to get at least one new value, but got '%v' and '%v'", val1, val2)
	}
}

func TestRandomizeTTL(t *testing.T) {
	rand.Seed(1)
	ttl := randomizeTTL(100 * time.Second)

	if ttl < 90*time.Second || ttl > 110*time.Second {
		t.Errorf("Expected to get ttl between 90 and 110 seconds, but got %v", ttl)
	}

	if int(ttl.Seconds()) != 103 {
		t.Errorf("Expected to get ttl equal to 103 seconds, but got %v", ttl)
	}
}

func TestCacheJSON(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage, CacheOptions{})

		// Define a test key and value
		key := "TestCacheJSONKey"
		value := map[string]string{
			"foo": "bar",
			"baz": "qux",
		}

		// Define a generator function that returns the test value
		generator := func() (any, error) {
			return value, nil
		}

		// Define a result variable to hold the unmarshalled JSON value
		var result map[string]string

		// Call the CacheJSON function to cache the test value
		err := cache.CacheJSON(key, generator, &result, CacheItemOptions{})

		// Check that the function returned no errors
		if err != nil {
			t.Errorf("%v: CacheJSON returned an error: %v", storageName, err)
		}

		// Check that the result variable contains the expected value
		if result["foo"] != "bar" || result["baz"] != "qux" {
			t.Errorf("%v: CacheJSON returned an unexpected value: %v", storageName, result)
		}

		// Call the CacheJSON function again to read value from storage
		err = cache.CacheJSON(key, generator, &result, CacheItemOptions{})

		// Check that the function returned no errors
		if err != nil {
			t.Errorf("%v: CacheJSON returned an error: %v", storageName, err)
		}

		// Check that the result variable contains the expected value
		if result["foo"] != "bar" || result["baz"] != "qux" {
			t.Errorf("%v: CacheJSON returned an unexpected value: %v", storageName, result)
		}
	}
}
