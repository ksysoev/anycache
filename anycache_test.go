package anycache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	memcachestor "github.com/ksysoev/anycache/storage/memcache"
	redisstor "github.com/ksysoev/anycache/storage/redis"
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
	redisStore := redisstor.NewRedisCacheStorage(redisClient)

	memcachedClient := memcache.New(getMemcachedHost())
	memcachedStore := memcachestor.NewMemcachedCacheStorage(memcachedClient)

	return map[string]CacheStorage{
		"redis":     redisStore,
		"memcached": memcachedStore,
	}
}

func getGenerator(val string, err error) CacheGenerator {
	return func(ctx context.Context) (string, error) {
		return val, err
	}
}

func TestCache(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage)

		val, err := cache.Cache("TestCacheKey", getGenerator("testValue", nil))

		if err != nil {
			t.Errorf("%v: Expected to get no error, but got %v", storageName, err)
		}

		if val != "testValue" {
			t.Errorf("%v: Expected to get testValue, but got '%v'", storageName, val)
		}

		val, err = cache.Cache("TestCacheKey", getGenerator("testValue1", nil))

		if err != nil {
			t.Errorf("%v: Expected to get no error, but got %v", storageName, err)
		}

		if val != "testValue" {
			t.Errorf("%v: Expected to get testValue, but got '%v'", storageName, val)
		}

		val, err = cache.Cache("TestCacheKey1", getGenerator("", errors.New("TestError")))

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
		cache := NewCache(cacheStorage)

		results := make(chan string)

		go func(c *Cache, ch chan string) {
			val, _ := c.Cache("TestCacheConcurrencyKey", func(ctx context.Context) (string, error) {
				time.Sleep(time.Millisecond)
				return "testValue", nil
			})
			ch <- val
		}(&cache, results)

		go func(c *Cache, ch chan string) {
			val, _ := c.Cache("TestCacheConcurrencyKey", func(ctx context.Context) (string, error) {
				time.Sleep(time.Millisecond)
				return "testValue1", nil
			})
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
	// For now, we test only Redis storage, Memcache client does not support TTL
	redisClient := redis.NewClient(getRedisOptions())
	cacheStore := redisstor.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore)

	val, err := cache.Cache("TestCacheWarmingUpKey", getGenerator("testValue", nil), WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	results := make(chan string)

	time.Sleep(time.Millisecond * 1001)

	go func(c *Cache, ch chan string) {
		val, err := c.Cache("TestCacheWarmingUpKey", func(ctx context.Context) (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))

		if err != nil {
			t.Errorf("Expected to get no error, but got %v", err)
		}
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
		val, err := c.Cache("TestCacheWarmingUpKey", func(ctx context.Context) (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))

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
	ttl := randomizeTTL(10, 100*time.Second)

	if ttl < 90*time.Second || ttl > 110*time.Second {
		t.Errorf("Expected to get ttl between 90 and 110 seconds, but got %v", ttl)
	}
}

func TestCacheJSON(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage)

		// Define a test key and value
		key := "TestCacheJSONKey"
		value := map[string]string{
			"foo": "bar",
			"baz": "qux",
		}

		// Define a generator function that returns the test value
		generator := func(ctx context.Context) (any, error) {
			return value, nil
		}

		// Define a result variable to hold the unmarshalled JSON value
		var result map[string]string

		// Call the CacheJSON function to cache the test value
		err := cache.CacheStruct(key, generator, &result)

		// Check that the function returned no errors
		if err != nil {
			t.Errorf("%v: CacheJSON returned an error: %v", storageName, err)
		}

		// Check that the result variable contains the expected value
		if result["foo"] != "bar" || result["baz"] != "qux" {
			t.Errorf("%v: CacheJSON returned an unexpected value: %v", storageName, result)
		}

		// Call the CacheJSON function again to read value from storage
		err = cache.CacheStruct(key, generator, &result)

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

func TestCancelingRequest(t *testing.T) {
	for storageName, cacheStorage := range getCacheStorages() {
		cache := NewCache(cacheStorage)

		// Define a generator function that returns the test value
		generator := func(ctx context.Context) (string, error) {
			time.Sleep(time.Millisecond * 500)
			return "testValue", nil
		}

		// Call the CacheJSON function to cache the test value
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)

		result, err := cache.Cache("TestCancelingRequestKey", generator, WithTTL(2*time.Second), WithCtx(ctx))

		// Check that the function returned no errors
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%v: Cache returned unexpected error: %v", storageName, err)
		}

		// Check that the result variable contains the expected value
		if result != "" {
			t.Errorf("%v: Cache returned an unexpected value: %v", storageName, result)
		}

		cancel()
	}
}

func TestPerfomance(t *testing.T) {
	const (
		TestRedisHost     = "localhost"
		TestRedisPort     = "6379"
		MaxConcurrency    = 10000
		RequestsPerThread = 10
		NumberOfKeys      = 400
	)

	var expectedResults = map[string]time.Duration{
		// For Github Actions we have to increase expected timing, on local machine it runs >20X faster
		"redis":     7 * time.Second,
		"memcached": 10 * time.Second,
	}

	for storageName, cacheStorage := range getCacheStorages() {
		var wg sync.WaitGroup

		cache := NewCache(cacheStorage)
		startTime := time.Now()

		for i := 0; i < MaxConcurrency; i++ {
			wg.Add(1)

			go func() {
				defer wg.Done()

				for i := 0; i < RequestsPerThread; i++ {
					//nolint:gosec // we don't need cryptographically secure random number generator for tesst
					key := fmt.Sprintf("key%d", rand.Intn(NumberOfKeys))
					_, err := cache.Cache(key, getGenerator(fmt.Sprintf("value%s", key), nil))

					if err != nil {
						fmt.Printf("Error caching value for key %s: %v\n", key, err)
						continue
					}
				}
			}()
		}

		wg.Wait()

		if time.Since(startTime) > expectedResults[storageName] {
			numberOfRequests := MaxConcurrency * RequestsPerThread
			t.Errorf("Total time of execution for %s: %d requests per  %v\n", storageName, numberOfRequests, time.Since(startTime))
		}

		fmt.Printf("Total time of execution for %s: %v\n", storageName, time.Since(startTime))
	}
}
