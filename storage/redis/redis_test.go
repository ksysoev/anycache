package redis

import (
	"errors"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newStringResult(t *testing.T, val string, err error) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(t.Context())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}

	return cmd
}

func newStatusResult(t *testing.T, err error) *goredis.StatusCmd {
	cmd := goredis.NewStatusCmd(t.Context())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal("OK")
	}

	return cmd
}

func newIntResult(t *testing.T, val int64, err error) *goredis.IntCmd {
	cmd := goredis.NewIntCmd(t.Context())
	if err != nil {
		cmd.SetErr(err)
	} else {
		cmd.SetVal(val)
	}

	return cmd
}

func newDurationResult(t *testing.T, val time.Duration, err error) *goredis.DurationCmd {
	cmd := goredis.NewDurationCmd(t.Context(), time.Millisecond)
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

	mc.EXPECT().Get(mock.Anything, "mykey").Return(newStringResult(t, "hello", nil))

	val, err := s.Get(t.Context(), "mykey")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestGet_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "missing").Return(newStringResult(t, "", goredis.Nil))

	_, err := s.Get(t.Context(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestGet_PropagatesUnexpectedError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	connErr := errors.New("connection refused")
	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult(t, "", connErr))

	_, err := s.Get(t.Context(), "key")

	assert.ErrorIs(t, err, connErr)
}

func TestSet_NoTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Duration(0)).Return(newStatusResult(t, nil))

	err := s.Set(t.Context(), "key", []byte("value"), 0)

	require.NoError(t, err)
}

func TestSet_WithTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), 5*time.Second).Return(newStatusResult(t, nil))

	err := s.Set(t.Context(), "key", []byte("value"), 5*time.Second)

	require.NoError(t, err)
}

func TestSet_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	serverErr := errors.New("server error")
	mc.EXPECT().Set(mock.Anything, "key", []byte("value"), time.Duration(0)).Return(newStatusResult(t, serverErr))

	err := s.Set(t.Context(), "key", []byte("value"), 0)

	assert.ErrorIs(t, err, serverErr)
}

func TestTTL_KeyHasTTL(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(t, 5*time.Second, nil))

	hasTTL, ttl, err := s.ttl(t.Context(), "key")

	require.NoError(t, err)
	assert.True(t, hasTTL)
	assert.Equal(t, 5*time.Second, ttl)
}

func TestTTL_KeyHasNoExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	// -1 is the sentinel for "key exists, no expiry"
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(t, time.Duration(-1), nil))

	hasTTL, _, err := s.ttl(t.Context(), "key")

	require.NoError(t, err)
	assert.False(t, hasTTL)
}

func TestTTL_KeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	// -2 is the sentinel for "key does not exist"
	mc.EXPECT().PTTL(mock.Anything, "missing").Return(newDurationResult(t, time.Duration(-2), nil))

	_, _, err := s.ttl(t.Context(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestTTL_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	redisErr := errors.New("redis error")
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(t, 0, redisErr))

	_, _, err := s.ttl(t.Context(), "key")

	assert.ErrorIs(t, err, redisErr)
}

func TestDel_ExistingKey(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Del(mock.Anything, "key").Return(newIntResult(t, 1, nil))

	deleted, err := s.Del(t.Context(), "key")

	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestDel_MissingKey(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Del(mock.Anything, "missing").Return(newIntResult(t, 0, nil))

	deleted, err := s.Del(t.Context(), "missing")

	require.NoError(t, err)
	assert.False(t, deleted)
}

func TestDel_PropagatesError(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	netErr := errors.New("network error")
	mc.EXPECT().Del(mock.Anything, "key").Return(newIntResult(t, 0, netErr))

	deleted, err := s.Del(t.Context(), "key")

	assert.ErrorIs(t, err, netErr)
	assert.False(t, deleted)
}

func TestGetWithTTL_HitNoExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult(t, "value", nil))
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(t, time.Duration(-1), nil))

	val, ttl, err := s.GetWithTTL(t.Context(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Zero(t, ttl)
}

func TestGetWithTTL_HitWithExpiry(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "key").Return(newStringResult(t, "value", nil))
	mc.EXPECT().PTTL(mock.Anything, "key").Return(newDurationResult(t, 3*time.Second, nil))

	val, ttl, err := s.GetWithTTL(t.Context(), "key")

	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
	assert.Equal(t, 3*time.Second, ttl)
}

func TestGetWithTTL_MissReturnsErrKeyNotExists(t *testing.T) {
	mc := NewMockClient(t)
	s := New(mc)

	mc.EXPECT().Get(mock.Anything, "missing").Return(newStringResult(t, "", goredis.Nil))

	_, _, err := s.GetWithTTL(t.Context(), "missing")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}
