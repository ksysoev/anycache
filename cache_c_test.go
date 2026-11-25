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
