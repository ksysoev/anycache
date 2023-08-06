package anycache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/ksysoev/anycache/storage/redis_storage"
	"github.com/redis/go-redis/v9"
)

func getRedisAddr() string {
	TestRedisHost := os.Getenv("TEST_REDIS_HOST")
	if TestRedisHost == "" {
		TestRedisHost = "localhost"
	}

	TestRedisPort := os.Getenv("TEST_REDIS_PORT")
	if TestRedisPort == "" {
		TestRedisPort = "6379"
	}

	return fmt.Sprintf("%s:%s", TestRedisHost, TestRedisPort)
}

// Map storage tests
func TestCache(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: getRedisAddr()})
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore, CacheOptions{})

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

	redisClient.FlushAll(context.Background())
}

func TestCacheConcurrency(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: getRedisAddr()})
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore, CacheOptions{})

	results := make(chan string)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue", nil
		}, CacheItemOptions{})
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
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

	redisClient.FlushAll(context.Background())
}

func TestCacheWarmingUp(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{Addr: getRedisAddr(), DB: 1})
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore, CacheOptions{})
	cacheOptions := CacheItemOptions{TTL: 3 * time.Second, WarmUpTTL: 2 * time.Second}

	val, _ := cache.Cache("testKey", func() (string, error) {
		return "testValue", nil
	}, cacheOptions)

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	results := make(chan string)

	time.Sleep(time.Millisecond * 1001)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, cacheOptions)
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond * 10)
			return "newTestValue", nil
		}, cacheOptions)
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

	redisClient.FlushAll(context.Background())
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
	// Create a new cache instance

	redisClient := redis.NewClient(&redis.Options{Addr: getRedisAddr()})
	cacheStore := redis_storage.NewRedisCacheStorage(redisClient)
	cache := NewCache(cacheStore, CacheOptions{})

	// Define a test key and value
	key := "test"
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
		t.Errorf("CacheJSON returned an error: %v", err)
	}

	// Check that the result variable contains the expected value
	if result["foo"] != "bar" || result["baz"] != "qux" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}

	// Call the CacheJSON function again to read value from storage
	err = cache.CacheJSON(key, generator, &result, CacheItemOptions{})

	// Check that the function returned no errors
	if err != nil {
		t.Errorf("CacheJSON returned an error: %v", err)
	}

	// Check that the result variable contains the expected value
	if result["foo"] != "bar" || result["baz"] != "qux" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}

	redisClient.FlushAll(context.Background())
}
