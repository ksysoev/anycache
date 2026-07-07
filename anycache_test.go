package anycache

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

//nolint:unparam // this function is used in tests and always returns the same value and error, so the parameters are not used.
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

func TestCache_WithShouldCache_SkipsStorageSet(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "skip-cache").Return(nil, ErrKeyNotExists).Twice()

	var generatorCalls atomic.Int32

	generator := func(_ context.Context) ([]byte, error) {
		call := generatorCalls.Add(1)
		return []byte(fmt.Sprintf("generated-%d", call)), nil
	}

	first, err := cache.Cache(t.Context(), "skip-cache", time.Second, generator, WithShouldCache(func([]byte) bool { return false }))
	assert.NoError(t, err)
	assert.Equal(t, []byte("generated-1"), first)

	second, err := cache.Cache(t.Context(), "skip-cache", time.Second, generator, WithShouldCache(func([]byte) bool { return false }))
	assert.NoError(t, err)
	assert.Equal(t, []byte("generated-2"), second)

	assert.Equal(t, int32(2), generatorCalls.Load(), "expected generator to run for each request when cache is bypassed")
}

func TestCache_WithShouldCache_DisablesSingleflight(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "skip-cache-concurrent").Return(nil, ErrKeyNotExists).Twice()

	var generatorCalls atomic.Int32

	started := make(chan struct{}, 2)
	release := make(chan struct{})

	//nolint:unparam // the parameters are not used in this test
	generator := func(_ context.Context) ([]byte, error) {
		call := generatorCalls.Add(1)

		started <- struct{}{}

		<-release

		return []byte(fmt.Sprintf("generated-%d", call)), nil
	}

	results := make(chan []byte, 2)
	errs := make(chan error, 2)

	for range 2 {
		go func() {
			val, err := cache.Cache(t.Context(), "skip-cache-concurrent", time.Second, generator, WithShouldCache(func([]byte) bool { return false }))
			results <- val

			errs <- err
		}()
	}

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("expected both generators to start")
		}
	}

	close(release)

	assert.NoError(t, <-errs)
	assert.NoError(t, <-errs)

	actual := []string{string(<-results), string(<-results)}
	assert.ElementsMatch(t, []string{"generated-1", "generated-2"}, actual)
	assert.Equal(t, int32(2), generatorCalls.Load(), "expected generator to run twice without singleflight de-duplication")
}

func TestCacheMetricHook_WarmUp(t *testing.T) {
	store := NewMockCacheStorage(t)

	var (
		observedState   State
		observedLatency time.Duration
	)

	cache := New(store, WithMetricHook(func(_ string, op State, latency time.Duration) {
		observedState = op
		observedLatency = latency
	}))

	store.EXPECT().GetWithTTL(mock.Anything, "metric-warmup").Return([]byte("cached"), 500*time.Millisecond, nil)
	store.EXPECT().Set(mock.Anything, "metric-warmup", []byte("fresh"), 2*time.Second).Return(nil)

	result, err := cache.Cache(t.Context(), "metric-warmup", 2*time.Second, getGenerator([]byte("fresh"), nil), WithWarmUpTTL(time.Second))

	assert.NoError(t, err)
	assert.Equal(t, []byte("cached"), result)
	assert.Equal(t, CacheWarmUp, observedState)
	assert.GreaterOrEqual(t, observedLatency, time.Duration(0))
	assert.NoError(t, cache.Close())
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

	key := "TestCacheJSONKey"
	value := map[string]string{
		"foo": "bar",
	}

	generator := func(_ context.Context) (any, error) {
		return value, nil
	}

	var result map[string]string

	err := cache.CacheStruct(t.Context(), key, time.Second, generator, &result)
	if err != nil {
		t.Errorf("CacheJSON returned an error: %v", err)
	}

	if result["foo"] != "bar" {
		t.Errorf("CacheJSON returned an unexpected value: %v", result)
	}
}

type testCodec struct {
	encodeFn func(value any) ([]byte, error)
	decodeFn func(data []byte, value any) error
}

func (tc testCodec) Encode(value any) ([]byte, error) {
	return tc.encodeFn(value)
}

func (tc testCodec) Decode(data []byte, value any) error {
	return tc.decodeFn(data, value)
}

func TestCacheStruct_UsesCustomCodec(t *testing.T) {
	store := NewMockCacheStorage(t)
	codec := testCodec{
		encodeFn: func(value any) ([]byte, error) {
			assert.Equal(t, map[string]string{"foo": "bar"}, value)
			return []byte("encoded"), nil
		},
		decodeFn: func(data []byte, value any) error {
			assert.Equal(t, []byte("encoded"), data)

			out, ok := value.(*map[string]string)
			assert.True(t, ok)

			(*out)["foo"] = "bar"

			return nil
		},
	}
	cache := New(store, WithCodec(codec))

	store.EXPECT().Get(mock.Anything, "custom-codec").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "custom-codec", []byte("encoded"), mock.Anything).Return(nil)

	result := map[string]string{}

	err := cache.CacheStruct(t.Context(), "custom-codec", time.Second, func(_ context.Context) (any, error) {
		return map[string]string{"foo": "bar"}, nil
	}, &result)

	assert.NoError(t, err)
	assert.Equal(t, "bar", result["foo"])
}

func TestCacheStruct_CustomCodecEncodeError(t *testing.T) {
	store := NewMockCacheStorage(t)
	codec := testCodec{
		encodeFn: func(_ any) ([]byte, error) {
			return nil, assert.AnError
		},
		decodeFn: func(_ []byte, _ any) error {
			return nil
		},
	}
	cache := New(store, WithCodec(codec))

	store.EXPECT().Get(mock.Anything, "custom-codec-encode-error").Return(nil, ErrKeyNotExists)

	err := cache.CacheStruct(t.Context(), "custom-codec-encode-error", time.Second, func(_ context.Context) (any, error) {
		return map[string]string{"foo": "bar"}, nil
	}, &map[string]string{})

	assert.ErrorIs(t, err, assert.AnError)
}

func TestCacheStruct_CustomCodecDecodeError(t *testing.T) {
	store := NewMockCacheStorage(t)
	codec := testCodec{
		encodeFn: func(_ any) ([]byte, error) {
			return []byte("encoded"), nil
		},
		decodeFn: func(_ []byte, _ any) error {
			return assert.AnError
		},
	}
	cache := New(store, WithCodec(codec))

	store.EXPECT().Get(mock.Anything, "custom-codec-decode-error").Return([]byte("encoded"), nil)

	err := cache.CacheStruct(t.Context(), "custom-codec-decode-error", time.Second, func(_ context.Context) (any, error) {
		t.Fatal("generator should not be called on cache hit")
		return nil, nil
	}, &map[string]string{})

	assert.ErrorIs(t, err, assert.AnError)
}

func TestCancelingRequest(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)
	store.EXPECT().Get(mock.Anything, "TestCancelingRequestKey").Return(nil, ErrKeyNotExists)

	generator := func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	result, err := cache.Cache(ctx, "TestCancelingRequestKey", 2*time.Second, generator)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, result, "Expected to get nil result, but got '%v'", result)

	assert.NoError(t, cache.Close())
}

func TestCache_DefaultContextDecouplesValues_NoWithTimeout(t *testing.T) {
	type ctxKey string

	const key ctxKey = "request-id"

	store := NewMockCacheStorage(t)
	cache := New(store)

	ctx := context.WithValue(context.Background(), key, "req-123")

	store.EXPECT().Get(mock.Anything, "ctx-forwarding").RunAndReturn(func(gotCtx context.Context, _ string) ([]byte, error) {
		assert.Nil(t, gotCtx.Value(key))
		return nil, ErrKeyNotExists
	})
	store.EXPECT().Set(mock.Anything, "ctx-forwarding", []byte("generated"), mock.Anything).RunAndReturn(func(gotCtx context.Context, _ string, _ []byte, _ time.Duration) error {
		assert.Nil(t, gotCtx.Value(key))
		return nil
	})

	result, err := cache.Cache(ctx, "ctx-forwarding", time.Second, func(gotCtx context.Context) ([]byte, error) {
		assert.Nil(t, gotCtx.Value(key))
		return []byte("generated"), nil
	})

	assert.NoError(t, err)
	assert.Equal(t, []byte("generated"), result)
}

func TestCache_DefaultContextCancellationPropagation_NoWithTimeout(t *testing.T) {
	t.Run("deadline context", func(t *testing.T) {
		store := NewMockCacheStorage(t)
		cache := New(store)

		store.EXPECT().Get(mock.Anything, "ctx-deadline").Return(nil, ErrKeyNotExists)

		type ctxKey string

		const key ctxKey = "request-id"

		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), key, "req-123"), 100*time.Millisecond)
		defer cancel()

		generatorStarted := make(chan struct{})

		result, err := cache.Cache(ctx, "ctx-deadline", time.Second, func(genCtx context.Context) ([]byte, error) {
			close(generatorStarted)
			assert.Nil(t, genCtx.Value(key), "expected deduplicated work context to be decoupled from caller values")
			_, hasDeadline := genCtx.Deadline()
			assert.True(t, hasDeadline, "expected caller deadline to be mirrored as internal timeout")
			<-genCtx.Done()

			return nil, genCtx.Err()
		})

		select {
		case <-generatorStarted:
		case <-time.After(time.Second):
			t.Fatal("generator did not start")
		}

		assert.Nil(t, result)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("manual cancel context", func(t *testing.T) {
		store := NewMockCacheStorage(t)
		cache := New(store)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		store.EXPECT().Get(mock.Anything, "ctx-cancel").Maybe().Return(nil, context.Canceled)

		result, err := cache.Cache(ctx, "ctx-cancel", time.Second, func(genCtx context.Context) ([]byte, error) {
			<-genCtx.Done()
			return nil, genCtx.Err()
		})

		assert.Nil(t, result)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestCache_WithTimeout_DecouplesWorkFromCallerContext(t *testing.T) {
	type ctxKey string

	const key ctxKey = "request-id"

	store := NewMockCacheStorage(t)
	cache := New(store)

	generatorStarted := make(chan struct{})
	releaseGenerator := make(chan struct{})
	setCalled := make(chan struct{})

	store.EXPECT().Get(mock.Anything, "timeout-decouple").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "timeout-decouple", []byte("generated"), mock.Anything).RunAndReturn(func(setCtx context.Context, _ string, _ []byte, _ time.Duration) error {
		assert.Nil(t, setCtx.Value(key), "expected internal context to be decoupled from caller values when WithTimeout is used")
		close(setCalled)

		return nil
	}).Once()

	callerCtx, cancelCaller := context.WithCancel(context.WithValue(context.Background(), key, "caller-value"))
	errCh := make(chan error, 1)

	go func() {
		_, err := cache.Cache(callerCtx, "timeout-decouple", time.Second, func(genCtx context.Context) ([]byte, error) {
			assert.Nil(t, genCtx.Value(key), "expected generator context to be decoupled from caller values when WithTimeout is used")
			close(generatorStarted)
			<-releaseGenerator

			return []byte("generated"), nil
		}, WithTimeout(300*time.Millisecond))
		errCh <- err
	}()

	select {
	case <-generatorStarted:
	case <-time.After(time.Second):
		t.Fatal("generator did not start")
	}

	cancelCaller()
	close(releaseGenerator)

	select {
	case <-setCalled:
	case <-time.After(time.Second):
		t.Fatal("storage set was not called after caller cancellation")
	}

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cache call did not return")
	}
}

func TestCache_WithTimeout_SingleflightContinuesAfterInitiatorCancel(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	var generatorCalls atomic.Int32

	generatorStarted := make(chan struct{})
	releaseGenerator := make(chan struct{})

	var getCalls atomic.Int32

	store.EXPECT().Get(mock.Anything, "timeout-singleflight").RunAndReturn(func(_ context.Context, _ string) ([]byte, error) {
		if getCalls.Add(1) == 1 {
			return nil, ErrKeyNotExists
		}

		return []byte("generated"), nil
	})
	store.EXPECT().Set(mock.Anything, "timeout-singleflight", []byte("generated"), mock.Anything).Return(nil).Maybe()

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorErrCh := make(chan error, 1)
	waiterValCh := make(chan []byte, 1)
	waiterErrCh := make(chan error, 1)

	go func() {
		_, err := cache.Cache(initiatorCtx, "timeout-singleflight", time.Second, func(_ context.Context) ([]byte, error) {
			generatorCalls.Add(1)
			close(generatorStarted)
			<-releaseGenerator

			return []byte("generated"), nil
		}, WithTimeout(300*time.Millisecond))
		initiatorErrCh <- err
	}()

	select {
	case <-generatorStarted:
	case <-time.After(time.Second):
		t.Fatal("generator did not start")
	}

	go func() {
		val, err := cache.Cache(t.Context(), "timeout-singleflight", time.Second, getGenerator([]byte("unused"), nil), WithTimeout(300*time.Millisecond))
		waiterValCh <- val

		waiterErrCh <- err
	}()

	cancelInitiator()
	close(releaseGenerator)

	select {
	case err := <-initiatorErrCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("initiator call did not return")
	}

	select {
	case err := <-waiterErrCh:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("waiter call did not return")
	}

	select {
	case val := <-waiterValCh:
		assert.Equal(t, []byte("generated"), val)
	case <-time.After(time.Second):
		t.Fatal("waiter value was not returned")
	}

	assert.Equal(t, int32(1), generatorCalls.Load(), "expected shared generation to run once")
}

func TestCacheMetricHook_Miss(t *testing.T) {
	store := NewMockCacheStorage(t)

	var (
		observedKey     string
		observedState   State
		observedLatency time.Duration
	)

	cache := New(store, WithMetricHook(func(key string, op State, latency time.Duration) {
		observedKey = key
		observedState = op
		observedLatency = latency
	}))

	store.EXPECT().Get(mock.Anything, "metric-miss").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "metric-miss", []byte("generated"), mock.Anything).Return(nil)

	result, err := cache.Cache(t.Context(), "metric-miss", time.Second, getGenerator([]byte("generated"), nil))

	assert.NoError(t, err)
	assert.Equal(t, []byte("generated"), result)
	assert.Equal(t, "metric-miss", observedKey)
	assert.Equal(t, CacheMiss, observedState)
	assert.GreaterOrEqual(t, observedLatency, time.Duration(0))
}

func TestCacheMetricHook_Hit(t *testing.T) {
	store := NewMockCacheStorage(t)

	var observedState State

	cache := New(store, WithMetricHook(func(_ string, op State, _ time.Duration) {
		observedState = op
	}))

	store.EXPECT().Get(mock.Anything, "metric-hit").Return([]byte("cached"), nil)

	result, err := cache.Cache(t.Context(), "metric-hit", time.Second, getGenerator([]byte("generated"), nil))

	assert.NoError(t, err)
	assert.Equal(t, []byte("cached"), result)
	assert.Equal(t, CacheHit, observedState)
}

func TestCacheMetricHook_Error(t *testing.T) {
	store := NewMockCacheStorage(t)

	var observedState State

	cache := New(store, WithMetricHook(func(_ string, op State, _ time.Duration) {
		observedState = op
	}))

	store.EXPECT().Get(mock.Anything, "metric-error").Return(nil, assert.AnError)

	result, err := cache.Cache(t.Context(), "metric-error", time.Second, getGenerator([]byte("generated"), nil))

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, CacheError, observedState)
}

func TestCacheMetricHook_KeyNotUsesPrefixedStorageKey(t *testing.T) {
	store := NewMockCacheStorage(t)

	var observedKey string

	cache := New(
		store,
		WithKeyPrefix("p::"),
		WithMetricHook(func(key string, _ State, _ time.Duration) {
			observedKey = key
		}),
	)

	store.EXPECT().Get(mock.Anything, "p::metric-key").Return(nil, ErrKeyNotExists)
	store.EXPECT().Set(mock.Anything, "p::metric-key", []byte("generated"), mock.Anything).Return(nil)

	_, err := cache.Cache(t.Context(), "metric-key", time.Second, getGenerator([]byte("generated"), nil))

	assert.NoError(t, err)
	assert.Equal(t, "metric-key", observedKey)
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

func TestCache_GeneratorPanicRecovered(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "panic-recovery").Return(nil, ErrKeyNotExists).Once()

	result, err := cache.Cache(t.Context(), "panic-recovery", time.Second, func(_ context.Context) ([]byte, error) {
		panic("boom")
	})

	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache.Cache panicked")
}

func TestCache_WarmUpLockReleasedWhenWarmUpNotNeeded(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().GetWithTTL(mock.Anything, "warmup-lock-release").Return([]byte("cached"), 5*time.Second, nil).Once()

	result, err := cache.Cache(t.Context(), "warmup-lock-release", 10*time.Second, getGenerator([]byte("fresh"), nil), WithWarmUpTTL(time.Second))

	assert.NoError(t, err)
	assert.Equal(t, []byte("cached"), result)

	_, exists := cache.warmUpLocks.Load("warmup-lock-release")
	assert.False(t, exists, "warm up lock should be released when warm up is not started")
}

func TestCache_WarmUpHonorsTimeout(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().GetWithTTL(mock.Anything, "warmup-timeout").Return([]byte("cached"), 200*time.Millisecond, nil).Once()
	store.EXPECT().Set(mock.Anything, "warmup-timeout", []byte("fresh"), mock.Anything).RunAndReturn(func(ctx context.Context, _ string, _ []byte, _ time.Duration) error {
		_, ok := ctx.Deadline()
		assert.True(t, ok, "expected warm up context with deadline")

		return nil
	}).Once()

	result, err := cache.Cache(t.Context(), "warmup-timeout", 3*time.Second, getGenerator([]byte("fresh"), nil), WithWarmUpTTL(time.Second), WithTimeout(50*time.Millisecond))

	assert.NoError(t, err)
	assert.Equal(t, []byte("cached"), result)
	assert.NoError(t, cache.Close())
}

func TestCache_WarmUpSetErrorIsHandled(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().GetWithTTL(mock.Anything, "warmup-set-error").Return([]byte("cached"), 200*time.Millisecond, nil).Once()
	store.EXPECT().Set(mock.Anything, "warmup-set-error", []byte("fresh"), mock.Anything).Return(assert.AnError).Once()

	result, err := cache.Cache(t.Context(), "warmup-set-error", 2*time.Second, getGenerator([]byte("fresh"), nil), WithWarmUpTTL(time.Second))

	assert.NoError(t, err)
	assert.Equal(t, []byte("cached"), result)
	assert.NoError(t, cache.Close())
}
