package anycache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

func getGenerator(val string, err error) CacheGenerator {
	return func(_ context.Context) (string, error) {
		return val, err
	}
}

func TestCache(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	store.EXPECT().Get(mock.Anything, "TestCacheKey").Return("TestCacheKey", ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "TestCacheKey", "testValue", mock.Anything).Return(nil)

	val, err := cache.Cache(t.Context(), "TestCacheKey", getGenerator("testValue", nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	store.EXPECT().Get(mock.Anything, "TestCacheKey").Return("testValue", nil)

	val, err = cache.Cache(t.Context(), "TestCacheKey", getGenerator("testValue1", nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}
}

func TestCacheConcurrency(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	results := make(chan string)

	store.EXPECT().Get(mock.Anything, "TestCacheConcurrencyKey").Return("", ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCacheConcurrencyKey", mock.Anything, mock.Anything).Return(nil)

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
}

func TestCacheWarmingUp(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)

	store.EXPECT().GetWithTTL(mock.Anything, "TestCacheWarmingUpKey").Return("testValue", 500*time.Millisecond, nil)
	store.EXPECT().Set(mock.Anything, "TestCacheWarmingUpKey", "newTestValue", 2*time.Second).Return(nil)

	val, err := cache.Cache(t.Context(), "TestCacheWarmingUpKey", getGenerator("newTestValue", nil), WithTTL(2*time.Second), WithWarmUpTTL(1*time.Second))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	time.Sleep(time.Millisecond)
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
	store.EXPECT().Get(mock.Anything, "TestCacheJSONKey").Return("", ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCacheJSONKey", "{\"foo\":\"bar\"}", mock.Anything).Return(nil)
	// Define a test key and value
	key := "TestCacheJSONKey"
	value := map[string]string{
		"foo": "bar",
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
	if result["foo"] != "bar" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}
}

func TestCancelingRequest(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := NewCache(store)
	store.EXPECT().Get(mock.Anything, "TestCancelingRequestKey").Return("", ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCancelingRequestKey", "testValue", mock.Anything).Return(nil)

	// Define a generator function that returns the test value
	generator := func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "testValue", nil
	}

	// Call the CacheJSON function to cache the test value
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*1)

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

	time.Sleep(time.Millisecond * 10) // watch to finish set on mock
}
