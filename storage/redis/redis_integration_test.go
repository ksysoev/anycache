package redis

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getRedisOptions() *goredis.Options {
	host := os.Getenv("TEST_REDIS_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("TEST_REDIS_PORT")
	if port == "" {
		port = "6379"
	}

	return &goredis.Options{Addr: fmt.Sprintf("%s:%s", host, port)}
}

// skipIfNoRedis pings the Redis server and skips the test when it is unreachable.
func skipIfNoRedis(t *testing.T) *goredis.Client {
	t.Helper()

	client := goredis.NewClient(getRedisOptions())

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skipping integration test: redis not available at %s: %v", getRedisOptions().Addr, err)
	}

	return client
}

func TestIntegration_Get_Hit(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "integ:get:hit", "hello", 0).Err())

	val, err := s.Get(ctx, "integ:get:hit")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestIntegration_Get_Miss(t *testing.T) {
	skipIfNoRedis(t)

	s := New(goredis.NewClient(getRedisOptions()))
	ctx := context.Background()

	_, err := s.Get(ctx, "integ:get:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Set_NoTTL(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:set:nottl", []byte("persistent"), 0))

	val, err := client.Get(ctx, "integ:set:nottl").Result()

	require.NoError(t, err)
	assert.Equal(t, "persistent", val)
}

func TestIntegration_Set_WithTTL(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:set:ttl", []byte("expiring"), 2*time.Second))

	val, err := client.Get(ctx, "integ:set:ttl").Result()

	require.NoError(t, err)
	assert.Equal(t, "expiring", val)

	ttl, err := client.TTL(ctx, "integ:set:ttl").Result()

	require.NoError(t, err)
	assert.Greater(t, ttl.Milliseconds(), int64(0))
	assert.LessOrEqual(t, ttl.Milliseconds(), int64(2000))
}

func TestIntegration_GetWithTTL_NoExpiry(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:getttl:noexpiry", []byte("data"), 0))

	val, ttl, err := s.GetWithTTL(ctx, "integ:getttl:noexpiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Zero(t, ttl, "expected zero TTL for item stored without expiry")
}

func TestIntegration_GetWithTTL_WithExpiry(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:getttl:expiry", []byte("data"), 10*time.Second))

	val, ttl, err := s.GetWithTTL(ctx, "integ:getttl:expiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Greater(t, ttl, time.Duration(0), "expected positive TTL for item stored with expiry")
	assert.LessOrEqual(t, ttl, 10*time.Second)
}

func TestIntegration_GetWithTTL_Miss(t *testing.T) {
	skipIfNoRedis(t)

	s := New(goredis.NewClient(getRedisOptions()))
	ctx := context.Background()

	_, _, err := s.GetWithTTL(ctx, "integ:getttl:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_ExistingKey(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "integ:del:existing", "to-be-deleted", 0).Err())

	deleted, err := s.Del(ctx, "integ:del:existing")

	require.NoError(t, err)
	assert.True(t, deleted, "expected deleted=true for existing key")

	_, getErr := s.Get(ctx, "integ:del:existing")

	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_MissingKey(t *testing.T) {
	skipIfNoRedis(t)

	s := New(goredis.NewClient(getRedisOptions()))
	ctx := context.Background()

	deleted, err := s.Del(ctx, "integ:del:missing:nonexistent")

	require.NoError(t, err)
	assert.False(t, deleted, "expected deleted=false for non-existent key")
}

func TestIntegration_TTL_KeyHasTTL(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "integ:ttl:has", "value", 1*time.Second).Err())

	hasTTL, ttl, err := s.ttl(ctx, "integ:ttl:has")

	require.NoError(t, err)
	assert.True(t, hasTTL)
	assert.Greater(t, ttl.Milliseconds(), int64(0))
	assert.LessOrEqual(t, ttl.Milliseconds(), int64(1000))
}

func TestIntegration_TTL_KeyHasNoExpiry(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, client.Set(ctx, "integ:ttl:noexpiry", "value", 0).Err())

	hasTTL, _, err := s.ttl(ctx, "integ:ttl:noexpiry")

	require.NoError(t, err)
	assert.False(t, hasTTL)
}

func TestIntegration_TTL_KeyNotExists(t *testing.T) {
	skipIfNoRedis(t)

	s := New(goredis.NewClient(getRedisOptions()))
	ctx := context.Background()

	_, _, err := s.ttl(ctx, "integ:ttl:missing:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_RoundTrip_BinaryValue(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	binary := []byte{0x00, 0xFF, 0x0A, 0x1B, 0x2C}

	require.NoError(t, s.Set(ctx, "integ:roundtrip:binary", binary, 0))

	val, err := s.Get(ctx, "integ:roundtrip:binary")

	require.NoError(t, err)
	assert.Equal(t, binary, val)
}

func TestIntegration_RoundTrip_Overwrite(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:roundtrip:overwrite", []byte("first"), 0))
	require.NoError(t, s.Set(ctx, "integ:roundtrip:overwrite", []byte("second"), 0))

	val, err := s.Get(ctx, "integ:roundtrip:overwrite")

	require.NoError(t, err)
	assert.Equal(t, []byte("second"), val, "expected most-recent value after overwrite")
}

func TestIntegration_DeleteThenGet(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:edge:delthenget", []byte("exists"), 0))

	deleted, err := s.Del(ctx, "integ:edge:delthenget")

	require.NoError(t, err)
	require.True(t, deleted)

	_, getErr := s.Get(ctx, "integ:edge:delthenget")

	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_GetWithTTL_AfterDel(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:edge:getttl:del", []byte("v"), 10*time.Second))

	_, err := s.Del(ctx, "integ:edge:getttl:del")

	require.NoError(t, err)

	_, _, getErr := s.GetWithTTL(ctx, "integ:edge:getttl:del")

	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_EmptyValue(t *testing.T) {
	client := skipIfNoRedis(t)
	s := New(client)
	ctx := context.Background()

	require.NoError(t, s.Set(ctx, "integ:edge:empty", []byte{}, 0))

	val, err := s.Get(ctx, "integ:edge:empty")

	require.NoError(t, err)

	// Redis stores empty string; we get back an empty byte slice.
	assert.True(t, errors.Is(err, nil))
	assert.Empty(t, val)
}
