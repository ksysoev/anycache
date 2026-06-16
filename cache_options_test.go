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
		return ttl >= 90*time.Second && ttl <= 110*time.Second
	})).Return(nil)

	_, err := cache.Cache(t.Context(), "TestKey", func(_ context.Context) ([]byte, error) {
		return []byte("testValue"), nil
	}, WithTTL(100*time.Second))

	assert.NoError(t, err, "Expected to get no error, but got %v", err)
}

func TestWithWarmUpTTL_MaxTTLShift(t *testing.T) {
	option := WithTTLRandomization(20)
	storage := NewMockCacheStorage(t)
	cache := New(storage, option)

	assert.Equal(t, uint8(20), cache.maxShiftTTL, "Expected TTL randomization factor to be 20 percent, but got %v", cache.maxShiftTTL)

	option = WithTTLRandomization(100)
	cache = New(storage, option)

	assert.Equal(t, uint8(100), cache.maxShiftTTL, "Expected TTL randomization factor to be 100 percent, but got %v", cache.maxShiftTTL)

	option = WithTTLRandomization(150)
	cache = New(storage, option)
	assert.Equal(t, uint8(100), cache.maxShiftTTL, "Expected TTL randomization factor to be capped at 100 percent, but got %v", cache.maxShiftTTL)
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
