package anycache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// joinGrace covers the few instructions between a caller entering CacheC and
// joining the in-flight call: singleflight exposes no waiter count to wait on.
const joinGrace = 10 * time.Millisecond

// runCallers runs call(0) as the leader and, once the generator started,
// call(1..callers-1) as followers. release is closed after every follower
// entered CacheC, then runCallers waits for all calls to return.
func runCallers(t *testing.T, callers int, started <-chan struct{}, release chan struct{}, call func(i int)) {
	t.Helper()

	var (
		wg      sync.WaitGroup
		entered atomic.Int32
	)

	wg.Go(func() { call(0) })

	<-started

	for i := 1; i < callers; i++ {
		wg.Go(func() {
			entered.Add(1)
			call(i)
		})
	}

	require.Eventually(t, func() bool { return int(entered.Load()) == callers-1 }, 5*time.Second, time.Millisecond)
	time.Sleep(joinGrace)
	close(release)
	wg.Wait()
}

func TestCacheC(t *testing.T) {
	errGen := errors.New("generator failed")

	tests := []struct {
		setup     func(store *MockCacheStorage)
		genErr    error
		name      string
		wantErr   error
		genData   []byte
		wantData  []byte
		cacheable bool
	}{
		{
			name: "miss stores cacheable value",
			setup: func(store *MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
				store.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Second).Return(nil).Once()
			},
			genData:   []byte("value"),
			cacheable: true,
			wantData:  []byte("value"),
		},
		{
			name: "miss does not store non-cacheable value",
			setup: func(store *MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
			},
			genData:   []byte("value"),
			cacheable: false,
			wantData:  []byte("value"),
		},
		{
			name: "hit returns stored value",
			setup: func(store *MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, "key").Return([]byte("stored"), nil).Once()
			},
			genData:   []byte("value"),
			cacheable: true,
			wantData:  []byte("stored"),
		},
		{
			name: "generator error is returned",
			setup: func(store *MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
			},
			genErr:  errGen,
			wantErr: errGen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewMockCacheStorage(t)
			tt.setup(store)

			cache := New(store)

			got, err := cache.CacheC(t.Context(), "key", time.Second, func(context.Context) ([]byte, bool, error) {
				return tt.genData, tt.cacheable, tt.genErr
			})

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.wantData, got)
		})
	}
}

func TestCacheC_InvalidTTL(t *testing.T) {
	cache := New(NewMockCacheStorage(t))

	_, err := cache.CacheC(t.Context(), "key", 0, func(context.Context) ([]byte, bool, error) {
		return nil, true, nil
	})

	assert.Error(t, err)
}

func TestCacheC_DeDuplicatesNonCacheable(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()

	var calls atomic.Int32

	release := make(chan struct{})
	started := make(chan struct{})

	//nolint:unparam // generator signature requires an error result
	gen := func(context.Context) ([]byte, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
		}

		<-release

		return []byte("value"), false, nil
	}

	const callers = 5

	results := make([][]byte, callers)

	runCallers(t, callers, started, release, func(i int) {
		val, err := cache.CacheC(t.Context(), "key", time.Second, gen)
		assert.NoError(t, err)

		results[i] = val
	})

	assert.Equal(t, int32(1), calls.Load())

	for _, r := range results {
		assert.Equal(t, []byte("value"), r)
	}
}

func TestCacheC_WarmUpRespectsCacheable(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().GetWithTTL(mock.Anything, "key").Return([]byte("old"), 100*time.Millisecond, nil).Once()

	done := make(chan struct{})

	val, err := cache.CacheC(t.Context(), "key", time.Second, func(context.Context) ([]byte, bool, error) {
		defer close(done)

		return []byte("new"), false, nil
	}, WithWarmUpTTL(time.Second))

	require.NoError(t, err)
	assert.Equal(t, []byte("old"), val)

	<-done
	require.NoError(t, cache.Close())
	// mock asserts that Set was never called
}

func TestCacheC_IgnoresShouldCache(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Second).Return(nil).Once()

	val, err := cache.CacheC(t.Context(), "key", time.Second, func(context.Context) ([]byte, bool, error) {
		return []byte("value"), true, nil
	}, WithShouldCache(func(any) bool { return false }))

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestCacheC_SetErrorIsNotFatal(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Second).Return(assert.AnError).Once()

	var calls atomic.Int32

	release := make(chan struct{})
	started := make(chan struct{})

	//nolint:unparam // generator signature requires an error result
	gen := func(context.Context) ([]byte, bool, error) {
		if calls.Add(1) == 1 {
			close(started)
		}

		<-release

		return []byte("value"), true, nil
	}

	const callers = 3

	runCallers(t, callers, started, release, func(int) {
		val, err := cache.CacheC(t.Context(), "key", time.Second, gen)
		assert.NoError(t, err)
		assert.Equal(t, []byte("value"), val)
	})

	assert.Equal(t, int32(1), calls.Load(), "followers must share the generated value")
}

func TestCache_SetErrorIsStillReturned(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Second).Return(assert.AnError).Once()

	_, err := cache.Cache(t.Context(), "key", time.Second, func(context.Context) ([]byte, error) {
		return []byte("value"), nil
	})

	assert.ErrorIs(t, err, assert.AnError)
}

func TestCacheC_StoreGetsFreshTimeout(t *testing.T) {
	store := NewMockCacheStorage(t)
	cache := New(store)

	const timeout = 50 * time.Millisecond

	store.EXPECT().Get(mock.Anything, "key").Return(nil, ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Second).
		RunAndReturn(func(ctx context.Context, _ string, _ []byte, _ time.Duration) error {
			deadline, ok := ctx.Deadline()

			assert.NoError(t, ctx.Err(), "store must not inherit the exhausted generator budget")
			assert.True(t, ok, "store must still be bounded by the timeout")
			assert.Greater(t, time.Until(deadline), timeout/2)

			return nil
		}).Once()

	gen := func(ctx context.Context) ([]byte, bool, error) {
		// Use up the whole budget, like a handler finishing right at its deadline.
		<-ctx.Done()

		return []byte("value"), true, nil
	}

	val, err := cache.CacheC(t.Context(), "key", time.Second, gen, WithTimeout(timeout))

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestStoreContext(t *testing.T) {
	const tolerance = 100 * time.Millisecond

	tests := []struct {
		name        string
		genTimeout  time.Duration
		timeout     time.Duration
		wantTimeout time.Duration
	}{
		{name: "generator budget left is kept", genTimeout: 10 * time.Second, timeout: 10 * time.Second, wantTimeout: 10 * time.Second},
		{name: "exhausted budget gets the grace period", genTimeout: time.Millisecond, timeout: time.Minute, wantTimeout: storeGrace},
		{name: "grace period is capped at the timeout", genTimeout: time.Millisecond, timeout: 300 * time.Millisecond, wantTimeout: 300 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genCtx, cancelGen := context.WithTimeout(t.Context(), tt.genTimeout)
			defer cancelGen()

			ctx, cancel := storeContext(genCtx, t.Context(), tt.timeout)
			defer cancel()

			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			assert.InDelta(t, tt.wantTimeout, time.Until(deadline), float64(tolerance))
		})
	}

	t.Run("no timeout uses base", func(t *testing.T) {
		ctx, cancel := storeContext(t.Context(), t.Context(), 0)
		defer cancel()

		_, ok := ctx.Deadline()
		assert.False(t, ok)
	})

	t.Run("cancelled with base", func(t *testing.T) {
		base, cancelBase := context.WithCancel(t.Context())

		ctx, cancel := storeContext(base, base, time.Minute)
		defer cancel()

		cancelBase()
		assert.Error(t, ctx.Err())
	})
}
