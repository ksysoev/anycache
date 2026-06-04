package anycache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func getGenerator(val string, err error) CacheGenerator {
	return func(_ context.Context) (string, error) {
		return val, err
	}
}

func TestCache(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	val, err := cache.Cache(t.Context(), "TestCacheKey", getGenerator("testValue", nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache(t.Context(), "TestCacheKey", getGenerator("testValue1", nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache(t.Context(), "TestCacheKey1", getGenerator("", errors.New("TestError")))

	if err == errors.New("TestError") {
		t.Errorf("Expected to get TestError, but got %v", err)
	}

	if val != "" {
		t.Errorf("Expected to get empty string, but got '%v'", val)
	}

	if err := cache.Close(); err != nil {
		t.Errorf("Close returned an error: %v", err)
	}
}

func TestCacheConcurrency(t *testing.T) {
	storage := NewMockCacheStorage(t)
	cache := NewCache(storage)

	results := make(chan string)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache(t.Context(), "TestCacheConcurrencyKey", func(_ context.Context) (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue", nil
		})
		ch <- val
	}(cache, results)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache(t.Context(), "TestCacheConcurrencyKey", func(_ context.Context) (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue1", nil
		})
		ch <- val
	}(cache, results)

	val1, val2 := <-results, <-results

	if val1 != "testValue" && val1 != "testValue1" {
		t.Errorf("Expected to get testValue as a result, but got '%v'", val1)
	}

	if val1 != val2 {
		t.Errorf("Expected to get same result for concurent requests, but got '%v' and '%v", val1, val2)
	}

	if err := cache.Close(); err != nil {
		t.Errorf("Close returned an error: %v", err)
	}
}

func TestCacheWarmingUp(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	val, err := cache.Cache(t.Context(), "TestCacheWarmingUpKey", getGenerator("testValue", nil), WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	time.Sleep(time.Millisecond * 1001)

	val, err = cache.Cache(t.Context(), "TestCacheWarmingUpKey", func(_ context.Context) (string, error) {
		time.Sleep(time.Millisecond * 10)
		return "newTestValue", nil
	}, WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	time.Sleep(time.Millisecond * 50)

	val, err = cache.Cache(t.Context(), "TestCacheWarmingUpKey", getGenerator("testValue", nil), WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "newTestValue" {
		t.Errorf("Expected to get newTestValue, but got '%v'", val)
	}
}

func TestRandomizeTTL(t *testing.T) {
	ttl := randomizeTTL(10, 100*time.Second)

	if ttl < 90*time.Second || ttl > 110*time.Second {
		t.Errorf("Expected to get ttl between 90 and 110 seconds, but got %v", ttl)
	}
}

func TestCacheJSON(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	// Define a test key and value
	key := "TestCacheJSONKey"
	value := map[string]string{
		"foo": "bar",
		"baz": "qux",
	}

	// Define a generator function that returns the test value
	generator := func(_ context.Context) (any, error) {
		return value, nil
	}

	// Define a result variable to hold the unmarshalled JSON value
	var result map[string]string

	// Call the CacheJSON function to cache the test value
	err := cache.CacheStruct(t.Context(), key, generator, &result)
	// Check that the function returned no errors
	if err != nil {
		t.Errorf("CacheJSON returned an error: %v", err)
	}

	// Check that the result variable contains the expected value
	if result["foo"] != "bar" || result["baz"] != "qux" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}

	// Call the CacheJSON function again to read value from storage
	err = cache.CacheStruct(t.Context(), key, generator, &result)
	// Check that the function returned no errors
	if err != nil {
		t.Errorf("CacheJSON returned an error: %v", err)
	}

	// Check that the result variable contains the expected value
	if result["foo"] != "bar" || result["baz"] != "qux" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}

	if err := cache.Close(); err != nil {
		t.Errorf("Close returned an error: %v", err)
	}
}

func TestCancelingRequest(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	// Define a generator function that returns the test value
	generator := func(_ context.Context) (string, error) {
		time.Sleep(time.Millisecond * 500)
		return "testValue", nil
	}

	// Call the CacheJSON function to cache the test value
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)

	result, err := cache.Cache(ctx, "TestCancelingRequestKey", generator, WithTTL(2*time.Second))

	// Check that the function returned no errors
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Cache returned unexpected error: %v", err)
	}

	// Check that the result variable contains the expected value
	if result != "" {
		t.Errorf("Cache returned an unexpected value: %v", result)
	}

	cancel()

	if err := cache.Close(); err != nil {
		t.Errorf("Close returned an error: %v", err)
	}
}
