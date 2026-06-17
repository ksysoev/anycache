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

type Storage struct {
	memcache *memcache.Client
}

// New creates a new memcache cache storage with the provided memcache client.
func New(client *memcache.Client) *Storage {
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
// ttl <= 0 means the item never expires.
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
		// we trust that memcache will not return expired items
		// but incase app servers got incorrect time then we just return 0 TTl and let memcache expire the item on next access
		return item.Value, 0, nil
	}

	return item.Value, remaining, nil
}

// Del deletes key. Returns (false, nil) if the key did not exist.
func (s *Storage) Del(_ context.Context, key string) (bool, error) {
	err := s.memcache.Delete(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// encode serialises value + optional expiry into a protobuf CachedItem.
// expiresAt zero means no TTL.
func encode(value []byte, expiresAt time.Time) ([]byte, error) {
	item := &pb.CachedItem{Value: value}
	if !expiresAt.IsZero() {
		item.ExpiresAtUnix = expiresAt.Unix()
	}

	return proto.Marshal(item)
}

// decode deserialises a protobuf CachedItem from raw bytes.
func decode(raw []byte) (*pb.CachedItem, error) {
	item := &pb.CachedItem{}
	if err := proto.Unmarshal(raw, item); err != nil {
		return nil, fmt.Errorf("failed to decode cached item: %w", err)
	}

	return item, nil
}
