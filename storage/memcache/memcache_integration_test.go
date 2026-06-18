package memcache

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getMemcachedHost returns the memcached address from env vars, falling back to localhost:11211.
func getMemcachedHost() string {
	host := os.Getenv("TEST_MEMCACHED_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("TEST_MEMCACHED_PORT")
	if port == "" {
		port = "11211"
	}

	return fmt.Sprintf("%s:%s", host, port)
}

// skipIfNoMemcached pings the Memcached server and skips the test when it is unreachable.
func skipIfNoMemcached(t *testing.T) *memcache.Client {
	t.Helper()

	client := memcache.New(getMemcachedHost())
	if err := client.Ping(); err != nil {
		t.Skipf("skipping integration test: memcached not available at %s: %v", getMemcachedHost(), err)
	}

	return client
}

func TestIntegration_Get_Hit(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:get:hit", []byte("hello"), 0))

	val, err := s.Get(t.Context(), "integ:get:hit")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestIntegration_Get_Miss(t *testing.T) {
	skipIfNoMemcached(t)

	s := New(memcache.New(getMemcachedHost()))

	_, err := s.Get(t.Context(), "integ:get:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Set_NoTTL(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:set:nottl", []byte("persistent"), 0))

	val, err := s.Get(t.Context(), "integ:set:nottl")

	require.NoError(t, err)
	assert.Equal(t, []byte("persistent"), val)
}

func TestIntegration_Set_WithTTL(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:set:ttl", []byte("expiring"), 2*time.Second))

	// Item should exist immediately after setting.
	val, err := s.Get(t.Context(), "integ:set:ttl")
	require.NoError(t, err)
	assert.Equal(t, []byte("expiring"), val)
}

func TestIntegration_Set_TTLTooLarge(t *testing.T) {
	skipIfNoMemcached(t)

	s := New(memcache.New(getMemcachedHost()))

	err := s.Set(t.Context(), "integ:set:toolarge", []byte("value"), 31*24*time.Hour)

	assert.Error(t, err)
	assert.ErrorContains(t, err, "ttl value is too large")
}

func TestIntegration_GetWithTTL_NoExpiry(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:getttl:noexpiry", []byte("data"), 0))

	val, ttl, err := s.GetWithTTL(t.Context(), "integ:getttl:noexpiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Zero(t, ttl, "expected zero TTL for item stored without expiry")
}

func TestIntegration_GetWithTTL_WithExpiry(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:getttl:expiry", []byte("data"), 10*time.Second))

	val, ttl, err := s.GetWithTTL(t.Context(), "integ:getttl:expiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Greater(t, ttl, time.Duration(0), "expected positive TTL for item stored with expiry")
	assert.LessOrEqual(t, ttl, 10*time.Second)
}

func TestIntegration_GetWithTTL_Miss(t *testing.T) {
	skipIfNoMemcached(t)

	s := New(memcache.New(getMemcachedHost()))

	_, _, err := s.GetWithTTL(t.Context(), "integ:getttl:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_ExistingKey(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:del:existing", []byte("to-be-deleted"), 0))

	err := s.Del(t.Context(), "integ:del:existing")

	require.NoError(t, err)

	// Confirm the key is gone.
	_, getErr := s.Get(t.Context(), "integ:del:existing")

	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_MissingKey(t *testing.T) {
	skipIfNoMemcached(t)

	s := New(memcache.New(getMemcachedHost()))

	err := s.Del(t.Context(), "integ:del:missing:nonexistent")

	require.NoError(t, err)
}

func TestIntegration_RoundTrip_BinaryValue(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	// Store arbitrary binary data to verify protobuf encode/decode is transparent.
	binary := []byte{0x00, 0xFF, 0x0A, 0x1B, 0x2C}

	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:binary", binary, 0))

	val, err := s.Get(t.Context(), "integ:roundtrip:binary")

	require.NoError(t, err)
	assert.Equal(t, binary, val)
}

func TestIntegration_RoundTrip_Overwrite(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:overwrite", []byte("first"), 0))
	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:overwrite", []byte("second"), 0))

	val, err := s.Get(t.Context(), "integ:roundtrip:overwrite")

	require.NoError(t, err)
	assert.Equal(t, []byte("second"), val, "expected most-recent value after overwrite")
}

func TestIntegration_EmptyValue(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:edge:empty", []byte{}, 0))

	val, err := s.Get(t.Context(), "integ:edge:empty")

	require.NoError(t, err)

	var expected []byte // nil slice is semantically equivalent to empty slice for our purposes
	assert.Equal(t, expected, val)
}

func TestIntegration_DeleteThenGet(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:edge:delthenget", []byte("exists"), 0))

	err := s.Del(t.Context(), "integ:edge:delthenget")
	require.NoError(t, err)

	_, getErr := s.Get(t.Context(), "integ:edge:delthenget")
	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_GetWithTTL_AfterDel(t *testing.T) {
	client := skipIfNoMemcached(t)
	s := New(client)

	require.NoError(t, s.Set(t.Context(), "integ:edge:getttl:del", []byte("v"), 10*time.Second))

	err := s.Del(t.Context(), "integ:edge:getttl:del")
	require.NoError(t, err)

	_, _, getErr := s.GetWithTTL(t.Context(), "integ:edge:getttl:del")
	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

// Verify the raw Memcached client never sees a Set call when the TTL validation
// rejects the request before encoding.
func TestIntegration_Set_TTLTooSmall_Rejected(t *testing.T) {
	skipIfNoMemcached(t)

	s := New(memcache.New(getMemcachedHost()))

	// 999 ms rounds down to 0 seconds — must be rejected.
	err := s.Set(t.Context(), "integ:edge:smallttl", []byte("value"), 999*time.Millisecond)

	require.Error(t, err)
	assert.ErrorContains(t, err, "ttl value is too small")

	// Confirm nothing was written.
	_, getErr := s.Get(t.Context(), "integ:edge:smallttl")
	require.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}
