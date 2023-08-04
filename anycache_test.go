package anycache

import (
	"errors"
	"math/rand"
	"testing"
	"time"

	"github.com/ksysoev/anycache/storage"
)

// Map storage tests
func TestCache(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage()
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
}

func TestCacheConcurrency(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage()
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
}

func TestCacheWarmingUp(t *testing.T) {
	cacheStore := storage.NewMapCacheStorage()
	cache := NewCache(cacheStore, CacheOptions{})
	cacheOptions := CacheItemOptions{TTL: 2 * time.Millisecond, WarmUpTTL: time.Millisecond}

	val, _ := cache.Cache("testKey", func() (string, error) {
		return "testValue", nil
	}, cacheOptions)

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	results := make(chan string)

	time.Sleep(time.Millisecond)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "newTestValue", nil
		}, cacheOptions)
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
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
