package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newStringResult(val string, err error) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}

	return cmd
}

func newStatusResult(err error) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal("OK")
	}

	return cmd
}

func newIntResult(val int64, err error) *goredis.IntCmd {
	cmd := goredis.NewIntCmd(context.Background())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}

	return cmd
}

func newDurationResult(val time.Duration, err error) *goredis.DurationCmd {
	cmd := goredis.NewDurationCmd(context.Background(), time.Millisecond)
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}

	return cmd
}

func TestGet_HitReturnsValue(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "mykey").Return(newStringResult("hello", nil))

	val, err := s.Get(context.Background(), "mykey")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestGet_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "missing").Return(newStringResult("", goredis.Nil))

	_, err := s.Get(context.Background(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestGet_PropagatesUnexpectedError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	connErr := errors.New("connection refused")
	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult("", connErr))

	_, err := s.Get(context.Background(), "key")

	assert.ErrorIs(t, err, connErr)
}

func TestSet_NoTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Duration(0)).Return(newStatusResult(nil))

	err := s.Set(context.Background(), "key", []byte("value"), 0)

	require.NoError(t, err)
}

func TestSet_WithTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), 5*time.Second).Return(newStatusResult(nil))

	err := s.Set(context.Background(), "key", []byte("value"), 5*time.Second)

	require.NoError(t, err)
}

func TestSet_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	serverErr := errors.New("server error")
	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Duration(0)).Return(newStatusResult(serverErr))

	err := s.Set(context.Background(), "key", []byte("value"), 0)

	assert.ErrorIs(t, err, serverErr)
}

func TestTTL_KeyHasTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(5*time.Second, nil))

	hasTTL, ttl, err := s.ttl(context.Background(), "key")

	require.NoError(t, err)
	assert.True(t, hasTTL)
	assert.Equal(t, 5*time.Second, ttl)
}

func TestTTL_KeyHasNoExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	// -1 is the sentinel for "key exists, no expiry"
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(time.Duration(-1), nil))

	hasTTL, _, err := s.ttl(context.Background(), "key")

	require.NoError(t, err)
	assert.False(t, hasTTL)
}

func TestTTL_KeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	// -2 is the sentinel for "key does not exist"
	mc.EXPECT().PTTL(mock.Anything, "missing").Return(newDurationResult(time.Duration(-2), nil))

	_, _, err := s.ttl(context.Background(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestTTL_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	redisErr := errors.New("redis error")
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(0, redisErr))

	_, _, err := s.ttl(context.Background(), "key")

	assert.ErrorIs(t, err, redisErr)
}

func TestDel_ExistingKey(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Del(mock.Anything, "key").Return(newIntResult(1, nil))

	deleted, err := s.Del(context.Background(), "key")

	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestDel_MissingKey(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Del(mock.Anything, "missing").Return(newIntResult(0, nil))

	deleted, err := s.Del(context.Background(), "missing")

	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestDel_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	netErr := errors.New("network error")
	mc.EXPECT().Del(mock.Anything, "key").Return(newIntResult(0, netErr))

	deleted, err := s.Del(context.Background(), "key")

	assert.ErrorIs(t, err, netErr)
	assert.False(t, deleted)
}

func TestGetWithTTL_HitNoExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult("value", nil))
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(time.Duration(-1), nil))

	val, ttl, err := s.GetWithTTL(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Zero(t, ttl)
}

func TestGetWithTTL_HitWithExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult("value", nil))
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(3*time.Second, nil))

	val, ttl, err := s.GetWithTTL(context.Background(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Equal(t, 3*time.Second, ttl)
}

func TestGetWithTTL_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "missing").Return(newStringResult("", goredis.Nil))

	_, _, err := s.GetWithTTL(context.Background(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}
