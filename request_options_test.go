package anycache

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
)

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

func TestWithMetric_OverridesCacheMetricHook(t *testing.T) {
	store := NewMockCacheStorage(t)

	var (
		defaultHookCalls int
		requestHookCalls int
		observedKey      string
		observedState    State
		observedLatency  time.Duration
	)

	cache := New(store, WithMetricHook(func(_ string, _ State, _ time.Duration) {
		defaultHookCalls++
	}))

	requestHook := func(key string, op State, latency time.Duration) {
		requestHookCalls++
		observedKey = key
		observedState = op
		observedLatency = latency
	}

	store.EXPECT().Get(mock.Anything, "metric-override").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "metric-override", []byte("generated"), mock.Anything).Return(nil)

	result, err := cache.Cache(t.Context(), "metric-override", time.Second, getGenerator([]byte("generated"), nil), WithMetric(requestHook))

	assert.NoError(t, err)
	assert.Equal(t, []byte("generated"), result)
	assert.Equal(t, 1, requestHookCalls)
	assert.Equal(t, 0, defaultHookCalls)
	assert.Equal(t, "metric-override", observedKey)
	assert.Equal(t, CacheMiss, observedState)
	assert.GreaterOrEqual(t, observedLatency, time.Duration(0))
}

func TestWithMetric_PanicsOnNilHook(t *testing.T) {
	assert.PanicsWithValue(t, "metric hook cannot be nil", func() {
		WithMetric(nil)
	})
}
