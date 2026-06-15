package memcache

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
)

type Storage struct {
	memcache *memcache.Client
}

// NewRedisCacheStorage creates a new RedisCacheStorage
func New(client *memcache.Client) *Storage {
	return &Storage{client}
}

// Get retrieves the value associated with the provided key from the Memcached cache storage.
// It returns the value as a byte array and an error if any occurred.
// If the key does not exist, it returns a nil and a ErrKeyNotExists.
// If any other error occurs during the operation, it returns a nil and the error.
func (s *Storage) Get(_ context.Context, key string) ([]byte, error) {
	item, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, anycache.ErrKeyNotExists
	}

	if err != nil {
		return nil, err
	}

	return item.Value, nil
}

// Set stores a value associated with the provided key in the Memcached cache storage.
// It also accepts options which currently only includes TTL (time-to-live) for the key-value pair.
// If the TTL is greater than 0, the key-value pair will be automatically removed from the cache after the TTL duration.
// If the TTL is 0 or less, the key-value pair will persist in the cache until it is manually removed.
func (s *Storage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl.Seconds() > math.MaxInt32 {
		return errors.New("TTL value is too large")
	}

	if ttl > 0 {
		err := s.memcache.Set(&memcache.Item{Key: key, Value: value, Expiration: int32(ttl.Seconds())})
		if err != nil {
			return err
		}

		return nil
	}

	err := s.memcache.Set(&memcache.Item{Key: key, Value: value})
	if err != nil {
		return err
	}

	return nil
}

// TTL retrieves the time-to-live (TTL) for the provided key from the Memcached cache storage.
// It returns a boolean indicating whether the key has a TTL, the TTL as a time.Duration, and an error if any occurred.
// If the key does not exist, it returns false, a zero duration, and a ErrKeyNotExists.
// If the key exists but does not have an expiration, it returns false, a zero duration, and nil error.
// If the key exists and has an expiration, it returns true, the TTL, and nil error.
func (s *Storage) TTL(_ context.Context, key string) (bool, time.Duration, error) {
	var ttl time.Duration

	hasTTL := false
	item, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return hasTTL, ttl, anycache.ErrKeyNotExists
	}

	if err != nil {
		return hasTTL, ttl, err
	}

	if item.Expiration > 0 {
		return true, time.Duration(item.Expiration) * time.Second, nil
	}

	return false, 0, nil
}

// Del deletes the value associated with the provided key from the Memcached cache storage.
// It returns a boolean indicating whether the deletion was successful, and an error if any occurred.
// If the key does not exist or any other error occurs during the operation, it returns false and the error.
func (s *Storage) Del(_ context.Context, key string) (bool, error) {
	err := s.memcache.Delete(key)
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetWithTTL retrieves the value associated with the provided key from the Memcached cache storage.
// It returns the value as a string, the time-to-live (TTL) as a time.Duration, and an error if any occurred.
// Currently, the TTL is always returned as 0 because this function does not support retrieving the TTL from Memcached.
// If the key does not exist or any other error occurs during the operation, it returns the error and the TTL as 0.
func (s *Storage) GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return value, 0, err
	}

	return value, 0, nil
}
