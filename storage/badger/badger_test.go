package badger

import (
	"testing"
	"time"

	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/ksysoev/anycache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	db, err := badgerdb.Open(badgerdb.DefaultOptions(t.TempDir()).WithLogger(nil))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return New(db)
}

func TestIntegration_Get_Hit(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:get:hit", []byte("hello"), 0))

	val, err := s.Get(t.Context(), "integ:get:hit")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), val)
}

func TestIntegration_Get_Miss(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.Get(t.Context(), "integ:get:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Set_NoTTL(t *testing.T) {
	s := newTestStorage(t)

	err := s.Set(t.Context(), "integ:set:nottl", []byte("persistent"), 0)

	require.NoError(t, err)
}

func TestIntegration_Set_WithTTL(t *testing.T) {
	s := newTestStorage(t)

	err := s.Set(t.Context(), "integ:set:ttl", []byte("expiring"), 2*time.Second)

	require.NoError(t, err)
}

func TestIntegration_GetWithTTL_NoExpiry(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:getttl:noexpiry", []byte("data"), 0))

	val, ttl, err := s.GetWithTTL(t.Context(), "integ:getttl:noexpiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Zero(t, ttl)
}

func TestIntegration_GetWithTTL_WithExpiry(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:getttl:expiry", []byte("data"), 10*time.Second))

	val, ttl, err := s.GetWithTTL(t.Context(), "integ:getttl:expiry")

	require.NoError(t, err)
	assert.Equal(t, []byte("data"), val)
	assert.Greater(t, ttl, time.Duration(0))
	assert.LessOrEqual(t, ttl, 10*time.Second)
}

func TestIntegration_GetWithTTL_Miss(t *testing.T) {
	s := newTestStorage(t)

	_, _, err := s.GetWithTTL(t.Context(), "integ:getttl:miss:nonexistent")

	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_ExistingKey(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:del:existing", []byte("to-be-deleted"), 0))

	err := s.Del(t.Context(), "integ:del:existing")
	require.NoError(t, err)

	_, getErr := s.Get(t.Context(), "integ:del:existing")
	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}

func TestIntegration_Del_MissingKey(t *testing.T) {
	s := newTestStorage(t)

	err := s.Del(t.Context(), "integ:del:missing:nonexistent")

	require.NoError(t, err)
}

func TestIntegration_RoundTrip_BinaryValue(t *testing.T) {
	s := newTestStorage(t)
	binary := []byte{0x00, 0xFF, 0x0A, 0x1B, 0x2C}

	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:binary", binary, 0))

	val, err := s.Get(t.Context(), "integ:roundtrip:binary")

	require.NoError(t, err)
	assert.Equal(t, binary, val)
}

func TestIntegration_RoundTrip_Overwrite(t *testing.T) {
	s := newTestStorage(t)

	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:overwrite", []byte("first"), 0))
	require.NoError(t, s.Set(t.Context(), "integ:roundtrip:overwrite", []byte("second"), 0))

	val, err := s.Get(t.Context(), "integ:roundtrip:overwrite")

	require.NoError(t, err)
	assert.Equal(t, []byte("second"), val)
}

func TestIntegration_EmptyValue(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:edge:empty", []byte{}, 0))

	val, err := s.Get(t.Context(), "integ:edge:empty")

	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestIntegration_ItemExpiresAndThenMisses(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:ttl:expires", []byte("value"), time.Second))

	time.Sleep(1200 * time.Millisecond)

	_, err := s.Get(t.Context(), "integ:ttl:expires")
	assert.ErrorIs(t, err, anycache.ErrKeyNotExists)
}

func TestIntegration_GetWithTTL_AfterDel(t *testing.T) {
	s := newTestStorage(t)
	require.NoError(t, s.Set(t.Context(), "integ:edge:getttl:del", []byte("v"), 10*time.Second))

	err := s.Del(t.Context(), "integ:edge:getttl:del")
	require.NoError(t, err)

	_, _, getErr := s.GetWithTTL(t.Context(), "integ:edge:getttl:del")
	assert.ErrorIs(t, getErr, anycache.ErrKeyNotExists)
}
