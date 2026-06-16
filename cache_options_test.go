package anycache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestWithTTLRandomization(t *testing.T) {
	option := WithTTLRandomization(10)

	mockStorage := NewMockCacheStorage(t)
	cache := New(mockStorage, option)

	assert.Equal(t, uint8(10), cache.maxShiftTTL, "Expected TTL randomization factor to be 10 percent, but got %v", cache.maxShiftTTL)

	mockStorage.EXPECT().Get(mock.Anything, "TestKey").Return(nil, ErrKeyNotExists)
	mockStorage.EXPECT().Set(mock.Anything, "TestKey", []byte("testValue"), mock.MatchedBy(func(ttl time.Duration) bool {
		if ttl < 90*time.Second || ttl > 110*time.Second {
			t.Errorf("Expected TTL to be between 90s and 110s, but got %v", ttl)
			// don't return false here to allow test to continue, otherwise test will stuck
			return true
		}

		return true
	})).Return(nil)

	_, err := cache.Cache(t.Context(), "TestKey", func(_ context.Context) ([]byte, error) {
		return []byte("testValue"), nil
	}, WithTTL(100*time.Second))

	assert.NoError(t, err, "Expected to get no error, but got %v", err)
}

func TestWithKeyPrefix(t *testing.T) {
	option := WithKeyPrefix("testPrefix::")

	mockStorage := NewMockCacheStorage(t)
	cache := New(mockStorage, option)

	assert.Equal(t, "testPrefix::", cache.keyPrefix, "Expected key prefix to be 'testPrefix::', but got '%v'", cache.keyPrefix)

	mockStorage.EXPECT().Get(mock.Anything, "testPrefix::TestKey").Return(nil, ErrKeyNotExists)
	mockStorage.EXPECT().Set(mock.Anything, "testPrefix::TestKey", []byte("testValue"), mock.Anything).Return(nil)

	_, err := cache.Cache(t.Context(), "TestKey", func(_ context.Context) ([]byte, error) {
		return []byte("testValue"), nil
	})

	assert.NoError(t, err, "Expected to get no error, but got %v", err)

	mockStorage.EXPECT().Del(mock.Anything, "testPrefix::TestKey").Return(true, nil)

	err = cache.Invalidate(t.Context(), "TestKey")

	assert.NoError(t, err, "Expected to get no error, but got %v", err)
}
