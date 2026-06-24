package anycache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func getGenerator(val []byte, err error) CacheGenerator {
	return func(_ context.Context) ([]byte, error) {
		return val, err
	}
}

func TestCache(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "TestCacheKey").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "TestCacheKey", []byte("testValue"), mock.Anything).Return(nil)

	val, err := cache.Cache(t.Context(), "TestCacheKey", time.Second, getGenerator([]byte("testValue"), nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	assert.Equal(t, []byte("testValue"), val, "Expected to get testValue, but got '%v'", val)

	store.EXPECT().Get(mock.Anything, "TestCacheKey").Return([]byte("testValue"), nil)

	val, err = cache.Cache(t.Context(), "TestCacheKey", time.Second, getGenerator([]byte("testValue1"), nil))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	assert.Equal(t, []byte("testValue"), val, "Expected to get testValue, but got '%v'", val)
}

func TestCacheConcurrency(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	results := make(chan []byte)

	store.EXPECT().Get(mock.Anything, "TestCacheConcurrencyKey").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCacheConcurrencyKey", mock.Anything, mock.Anything).Return(nil)

	go func(c *Cache, ch chan []byte) {
		val, _ := c.Cache(t.Context(), "TestCacheConcurrencyKey", time.Second, func(_ context.Context) ([]byte, error) {
			time.Sleep(time.Millisecond)
			return []byte("testValue"), nil
		})
		ch <- val
	}(cache, results)

	go func(c *Cache, ch chan []byte) {
		val, _ := c.Cache(t.Context(), "TestCacheConcurrencyKey", time.Second, func(_ context.Context) ([]byte, error) {
			time.Sleep(time.Millisecond)
			return []byte("testValue1"), nil
		})
		ch <- val
	}(cache, results)

	val1, val2 := <-results, <-results

	assert.Contains(t, [][]byte{[]byte("testValue"), []byte("testValue1")}, val1, "Expected to get testValue or testValue1 as a result, but got '%v'", val1)
	assert.Contains(t, [][]byte{[]byte("testValue"), []byte("testValue1")}, val2, "Expected to get testValue or testValue1 as a result, but got '%v'", val2)
	assert.Equal(t, val1, val2, "Expected to get the same value for both calls, but got '%v' and '%v'", val1, val2)
}

func TestCacheWarmingUp(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().GetWithTTL(mock.Anything, "TestCacheWarmingUpKey").Return([]byte("testValue"), 500*time.Millisecond, nil)
	store.EXPECT().Set(mock.Anything, "TestCacheWarmingUpKey", []byte("newTestValue"), 2*time.Second).Return(nil)

	val, err := cache.Cache(t.Context(), "TestCacheWarmingUpKey", 2*time.Second, getGenerator([]byte("newTestValue"), nil), WithWarmUpTTL(1*time.Second))
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	assert.Equal(t, []byte("testValue"), val, "Expected to get testValue, but got '%v'", val)

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
	cache := New(store)
	store.EXPECT().Get(mock.Anything, "TestCacheJSONKey").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCacheJSONKey", []byte("{\"foo\":\"bar\"}"), mock.Anything).Return(nil)
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
	err := cache.CacheStruct(t.Context(), key, time.Second, generator, &result)
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
	cache := New(store)
	store.EXPECT().Get(mock.Anything, "TestCancelingRequestKey").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "TestCancelingRequestKey", []byte("testValue"), mock.Anything).Return(nil)
	// Define a generator function that returns the test value
	generator := func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return []byte("testValue"), nil
	}

	// Call the CacheJSON function to cache the test value
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*1)

	result, err := cache.Cache(ctx, "TestCancelingRequestKey", 2*time.Second, generator)
	// Check that the function returned no errors
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Cache returned unexpected error: %v", err)
	}

	// Check that the result variable contains the expected value
	assert.Nil(t, result, "Expected to get nil result, but got '%v'", result)

	cancel()

	if err := cache.Close(); err != nil {
		t.Errorf("Close returned an error: %v", err)
	}

	time.Sleep(time.Millisecond * 10) // watch to finish set on mock
}

func TestCache_Invalidate(t *testing.T) {
	tests := []struct {
		setup   func() CacheStorage
		name    string
		key     string
		wantErr bool
	}{
		{
			name:    "Invalidate key successfully",
			key:     "TestInvalidateKey",
			wantErr: false,
			setup: func() CacheStorage {
				store := NewMockCacheStorage(t)
				store.EXPECT().Del(mock.Anything, "TestInvalidateKey").Return(nil)

				return store
			},
		},
		{
			name:    "Invalidate key with error",
			key:     "TestInvalidateKey",
			wantErr: true,
			setup: func() CacheStorage {
				store := NewMockCacheStorage(t)
				store.EXPECT().Del(mock.Anything, "TestInvalidateKey").Return(assert.AnError)

				return store
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setup()
			c := New(store)

			gotErr := c.Invalidate(t.Context(), tt.key)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("Invalidate() failed: %v", gotErr)
				}

				return
			}

			if tt.wantErr {
				t.Fatal("Invalidate() succeeded unexpectedly")
			}
		})
	}
}
