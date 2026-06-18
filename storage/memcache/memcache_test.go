package memcache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// mustEncode is a test helper that serialises value+expiry into a protobuf payload.
func mustEncode(t *testing.T, value []byte, expiresAt time.Time) []byte {
	t.Helper()

	data, err := encode(value, expiresAt)
	require.NoError(t, err)

	return data
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestGet_HitReturnsValue(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	encoded := mustEncode(t, []byte("hello"), time.Time{})
	mc.EXPECT().Get("mykey").Return(&memcache.Item{Value: encoded}, nil)

	val, err := s.Get(context.Background(), "mykey")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestGet_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().Get("missing").Return(nil, memcache.ErrCacheMiss)

	_, err := s.Get(context.Background(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestGet_PropagatesUnexpectedError(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	connErr := errors.New("connection refused")
	mc.EXPECT().Get("key").Return(nil, connErr)

	_, err := s.Get(context.Background(), "key")

	assert.ErrorIs(t, err, connErr)
}

func TestGet_CorruptDataReturnsDecodeError(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().Get("key").Return(&memcache.Item{Value: []byte("not-valid-protobuf-!!!")}, nil)

	_, err := s.Get(context.Background(), "key")

	assert.Error(t, err)
	assert.ErrorContains(t, err, "failed to decode cached item")
}

// ---------------------------------------------------------------------------
// GetWithTTL
// ---------------------------------------------------------------------------

func TestGetWithTTL_NoExpiry(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	encoded := mustEncode(t, []byte("value"), time.Time{})
	mc.EXPECT().Get("key").Return(&memcache.Item{Value: encoded}, nil)

	val, ttl, err := s.GetWithTTL(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Zero(t, ttl)
}

func TestGetWithTTL_FutureExpiry(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	expiresAt := time.Now().Add(10 * time.Second)
	encoded := mustEncode(t, []byte("value"), expiresAt)
	mc.EXPECT().Get("key").Return(&memcache.Item{Value: encoded}, nil)

	val, ttl, err := s.GetWithTTL(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 10*time.Second)
}

func TestGetWithTTL_PastExpiry(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	// Simulate an item that expired 5 seconds ago (clock skew scenario).
	expiresAt := time.Now().Add(-5 * time.Second)
	encoded := mustEncode(t, []byte("value"), expiresAt)
	mc.EXPECT().Get("key").Return(&memcache.Item{Value: encoded}, nil)

	val, ttl, err := s.GetWithTTL(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Zero(t, ttl)
}

func TestGetWithTTL_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().Get("missing").Return(nil, memcache.ErrCacheMiss)

	_, _, err := s.GetWithTTL(context.Background(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestGetWithTTL_PropagatesUnexpectedError(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	connErr := errors.New("network timeout")
	mc.EXPECT().Get("key").Return(nil, connErr)

	_, _, err := s.GetWithTTL(context.Background(), "key")

	assert.ErrorIs(t, err, connErr)
}

// ---------------------------------------------------------------------------
// Set
// ---------------------------------------------------------------------------

func TestSet_NoTTL(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().
		Set(mock.MatchedBy(func(item *memcache.Item) bool {
			return item.Key == "key" && item.Expiration == 0
		})).
		Return(nil)

	err := s.Set(context.Background(), "key", []byte("value"), 0)

	require.NoError(t, err)
}

func TestSet_WithTTL(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().
		Set(mock.MatchedBy(func(item *memcache.Item) bool {
			return item.Key == "key" && item.Expiration == 5
		})).
		Return(nil)

	err := s.Set(context.Background(), "key", []byte("value"), 5*time.Second)

	require.NoError(t, err)
}

func TestSet_TTLTooLarge(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	// No mock expectation — Set should never reach the client.
	err := s.Set(context.Background(), "key", []byte("value"), 31*24*time.Hour)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "ttl value is too large")
}

func TestSet_TTLTooSmall(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	// 500 ms truncates to 0 seconds when cast to int32.
	err := s.Set(context.Background(), "key", []byte("value"), 500*time.Millisecond)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "ttl value is too small")
}

func TestSet_PropagatesMemcacheError(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	serverErr := errors.New("server error")
	mc.EXPECT().Set(mock.Anything).Return(serverErr)

	err := s.Set(context.Background(), "key", []byte("value"), 0)

	assert.ErrorIs(t, err, serverErr)
}

// ---------------------------------------------------------------------------
// Del
// ---------------------------------------------------------------------------

func TestDel_ExistingKey(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().Delete("key").Return(nil)

	deleted, err := s.Del(context.Background(), "key")

	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestDel_MissingKey(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	mc.EXPECT().Delete("missing").Return(memcache.ErrCacheMiss)

	deleted, err := s.Del(context.Background(), "missing")

	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestDel_PropagatesError(t *testing.T) {
	mc := NewMockMemCached(t)
	s := New(mc)

	netErr := errors.New("network error")
	mc.EXPECT().Delete("key").Return(netErr)

	deleted, err := s.Del(context.Background(), "key")

	assert.ErrorIs(t, err, netErr)
	assert.False(t, deleted)
}
