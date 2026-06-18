package layered

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorage_Del(t *testing.T) {
	key := "mykey"
	delErr := errors.New("delete failed")

	tests := []struct {
		setup   func(*testing.T) *Storage
		name    string
		wantErr bool
	}{
		{
			name: "key deleted from all layers returns true",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "key found only in first layer returns true",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "key found only in second layer returns true",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "key not found in any layer returns false",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "error in first layer stops iteration and returns false with error",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(delErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: true,
		},
		{
			name: "error in second layer after first deleted — returns true with error",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(delErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: true,
		},
		{
			name: "three layers all deleted returns true",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)
				s3 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(nil)
				s3.EXPECT().Del(context.Background(), key).Return(nil)

				store, err := New(s1, s2, s3)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "three layers error in middle layer after first deleted — returns true with error",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)
				s3 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Del(context.Background(), key).Return(nil)
				s2.EXPECT().Del(context.Background(), key).Return(delErr)

				store, err := New(s1, s2, s3)
				require.NoError(t, err)

				return store
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setup(t)

			err := store.Del(t.Context(), key)

			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorContains(t, err, delErr.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		stores  func(*testing.T) []anycache.CacheStorage
		name    string
		wantErr bool
	}{
		{
			name: "zero storages returns error",
			stores: func(t *testing.T) []anycache.CacheStorage {
				t.Helper()
				return nil
			},
			wantErr: true,
		},
		{
			name: "one storage returns error",
			stores: func(t *testing.T) []anycache.CacheStorage {
				t.Helper()
				return []anycache.CacheStorage{anycache.NewMockCacheStorage(t)}
			},
			wantErr: true,
		},
		{
			name: "two storages returns storage",
			stores: func(t *testing.T) []anycache.CacheStorage {
				t.Helper()

				return []anycache.CacheStorage{
					anycache.NewMockCacheStorage(t),
					anycache.NewMockCacheStorage(t),
				}
			},
			wantErr: false,
		},
		{
			name: "three storages returns storage",
			stores: func(t *testing.T) []anycache.CacheStorage {
				t.Helper()

				return []anycache.CacheStorage{
					anycache.NewMockCacheStorage(t),
					anycache.NewMockCacheStorage(t),
					anycache.NewMockCacheStorage(t),
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.stores(t)...)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, got)
			}
		})
	}
}

func TestStorage_Get(t *testing.T) {
	key := "mykey"
	val := []byte("myvalue")
	ttl := 5 * time.Minute

	tests := []struct {
		wantErr error
		setup   func(*testing.T) *Storage
		name    string
		wantVal []byte
	}{
		{
			name: "key found in first layer",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(val, ttl, nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: val,
			wantErr: nil,
		},
		{
			name: "key not found in any layer returns ErrKeyNotExists",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantErr: anycache.ErrKeyNotExists,
		},
		{
			name: "unexpected error from first layer is propagated",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, errors.New("connection refused"))

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantErr: errors.New("connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setup(t)

			got, err := store.Get(context.Background(), key)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVal, got)
			}
		})
	}
}

func TestStorage_Set(t *testing.T) {
	key := "mykey"
	val := []byte("myvalue")
	ttl := 5 * time.Minute
	storeErr := errors.New("write failed")

	tests := []struct {
		setup   func(*testing.T) *Storage
		name    string
		wantErr bool
	}{
		{
			name: "sets value in all layers",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)
				s2.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
		{
			name: "error in first layer stops propagation and returns error",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(storeErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: true,
		},
		{
			name: "error in second layer returns error",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)
				s2.EXPECT().Set(context.Background(), key, val, ttl).Return(storeErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantErr: true,
		},
		{
			name: "sets value in three layers",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)
				s3 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)
				s2.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)
				s3.EXPECT().Set(context.Background(), key, val, ttl).Return(nil)

				store, err := New(s1, s2, s3)
				require.NoError(t, err)

				return store
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setup(t)

			err := store.Set(context.Background(), key, val, ttl)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestStorage_GetWithTTL(t *testing.T) {
	key := "mykey"
	val := []byte("myvalue")
	ttl := 5 * time.Minute
	storeErr := fmt.Errorf("connection refused")

	tests := []struct {
		wantErr error
		setup   func(*testing.T) *Storage
		name    string
		wantVal []byte
		wantTTL time.Duration
	}{
		{
			name: "key found in first layer — no back-population",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(val, ttl, nil)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: val,
			wantTTL: ttl,
			wantErr: nil,
		},
		{
			name: "key found in second layer — back-populates first layer",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(val, ttl, nil)
				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(nil) // back-populate

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: val,
			wantTTL: ttl,
			wantErr: nil,
		},
		{
			name: "key found in third layer — back-populates second and first layers",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)
				s3 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s3.EXPECT().GetWithTTL(context.Background(), key).Return(val, ttl, nil)
				s2.EXPECT().Set(context.Background(), key, val, ttl).Return(nil) // back-populate layer 1
				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(nil) // back-populate layer 0

				store, err := New(s1, s2, s3)
				require.NoError(t, err)

				return store
			},
			wantVal: val,
			wantTTL: ttl,
			wantErr: nil,
		},
		{
			name: "key not found in any layer returns ErrKeyNotExists",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantTTL: 0,
			wantErr: anycache.ErrKeyNotExists,
		},
		{
			name: "unexpected error from first layer is propagated",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, storeErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantTTL: 0,
			wantErr: storeErr,
		},
		{
			name: "unexpected error from second layer is propagated",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, storeErr)

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantTTL: 0,
			wantErr: storeErr,
		},
		{
			name: "back-population error is returned",
			setup: func(t *testing.T) *Storage {
				t.Helper()
				s1 := anycache.NewMockCacheStorage(t)
				s2 := anycache.NewMockCacheStorage(t)

				s1.EXPECT().GetWithTTL(context.Background(), key).Return(nil, 0, anycache.ErrKeyNotExists)
				s2.EXPECT().GetWithTTL(context.Background(), key).Return(val, ttl, nil)
				s1.EXPECT().Set(context.Background(), key, val, ttl).Return(errors.New("set failed"))

				store, err := New(s1, s2)
				require.NoError(t, err)

				return store
			},
			wantVal: nil,
			wantTTL: 0,
			wantErr: errors.New("set failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setup(t)

			gotVal, gotTTL, err := store.GetWithTTL(context.Background(), key)

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVal, gotVal)
				assert.Equal(t, tt.wantTTL, gotTTL)
			}
		})
	}
}
