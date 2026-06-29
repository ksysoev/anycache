package memcache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
	pb "github.com/ksysoev/anycache/storage/memcache/proto"
	"google.golang.org/protobuf/proto"
)

const (
	maxTTL = 30 * 24 * time.Hour // 30 days, the maximum TTL supported by Memcached
)

// Storage implements anycache.CacheStorage on top of Memcached.
//
// Value format caveat:
//   - Values are always stored as protobuf CachedItem envelopes, not raw user bytes.
//   - Envelope fields: payload bytes + absolute expiry timestamp (expires_at_unix).
//   - This format is required so GetWithTTL can reconstruct remaining TTL.
//
// Interoperability caveat:
//   - External memcached readers/writers must use the same protobuf format.
//   - Raw-byte clients will not be wire-compatible with this backend by default.
//
// TTL caveat:
//   - ttl > 30 days is rejected.
//   - Positive ttl that truncates below 1 second is rejected.
//   - ttl <= 0 means no expiration.
type Storage struct {
	memcache MemCached
}

type MemCached interface {
	Get(key string) (*memcache.Item, error)
	Set(item *memcache.Item) error
	Delete(key string) error
}

// New creates a new memcache cache storage with the provided memcache client.
func New(client MemCached) *Storage {
	return &Storage{client}
}

// Get retrieves the value associated with the provided key from the Memcached cache storage.
// It returns the value as a byte array and an error if any occurred.
// If the key does not exist, it returns a nil and a ErrKeyNotExists.
// If any other error occurs during the operation, it returns a nil and the error.
func (s *Storage) Get(ctx context.Context, key string) ([]byte, error) {
	val, _, err := s.GetWithTTL(ctx, key)

	return val, err
}

// Set stores value under key with an optional TTL.
//
// The stored memcached payload is protobuf CachedItem, not raw value bytes,
// because absolute expiry is persisted for TTL reconstruction in GetWithTTL.
//
// TTL constraints:
//   - ttl > 30 days returns an error.
//   - ttl > 0 is truncated to whole seconds for memcached expiration.
//   - if truncated ttl is < 1 second, Set returns an error.
//   - ttl <= 0 means no expiration.
func (s *Storage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl > maxTTL {
		return errors.New("ttl value is too large for memcache. Maximum allowed is 30 days")
	}

	var (
		expiresAt  time.Time
		expSeconds int32
	)

	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
		expSeconds = int32(ttl.Seconds())

		if expSeconds <= 0 {
			return errors.New("ttl value is too small, must be at least 1 second")
		}
	}

	data, err := encode(value, expiresAt)
	if err != nil {
		return err
	}

	return s.memcache.Set(&memcache.Item{
		Key:        key,
		Value:      data,
		Expiration: expSeconds,
	})
}

// GetWithTTL retrieves both the value and its remaining TTL in one call.
//
// Remaining TTL is reconstructed from the protobuf CachedItem.expires_at_unix
// field written at Set time. When expires_at_unix == 0, the key has no TTL.
func (s *Storage) GetWithTTL(_ context.Context, key string) ([]byte, time.Duration, error) {
	raw, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, 0, anycache.ErrKeyNotExists
	}

	if err != nil {
		return nil, 0, err
	}

	item, err := decode(raw.Value)
	if err != nil {
		return nil, 0, err
	}

	if item.ExpiresAtUnix == 0 {
		return item.Value, 0, nil
	}

	expiresAt := time.Unix(item.ExpiresAtUnix, 0)
	remaining := time.Until(expiresAt)

	if remaining <= 0 {
		// We expect memcache not to return expired items.
		// If server clocks are skewed, return zero TTL and let memcache expire on next access.
		return item.Value, 0, nil
	}

	return item.Value, remaining, nil
}

// Del deletes the value associated with the provided key from the Memcached cache storage.
func (s *Storage) Del(_ context.Context, key string) error {
	err := s.memcache.Delete(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil
	}

	return err
}

// encode serializes value + optional absolute expiry into protobuf CachedItem.
// expiresAt zero means no TTL.
func encode(value []byte, expiresAt time.Time) ([]byte, error) {
	item := &pb.CachedItem{Value: value}
	if !expiresAt.IsZero() {
		item.ExpiresAtUnix = expiresAt.Unix()
	}

	return proto.Marshal(item)
}

// decode deserializes a protobuf CachedItem from raw bytes.
func decode(raw []byte) (*pb.CachedItem, error) {
	item := &pb.CachedItem{}
	if err := proto.Unmarshal(raw, item); err != nil {
		return nil, fmt.Errorf("failed to decode cached item: %w", err)
	}

	return item, nil
}
