package memcachestor

import (
	"context"
	"errors"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache/storage"
)

type MemcachedCacheStorage struct {
	memcache *memcache.Client
}

// NewRedisCacheStorage creates a new RedisCacheStorage
func NewMemcachedCacheStorage(memcache *memcache.Client) *MemcachedCacheStorage {
	return &MemcachedCacheStorage{memcache}
}

// Get retrieves the value associated with the provided key from the Memcached cache storage.
// It returns the value as a string and an error if any occurred.
// If the key does not exist, it returns an empty string and a KeyNotExistError.
// If any other error occurs during the operation, it returns an empty string and the error.
func (s *MemcachedCacheStorage) Get(ctx context.Context, key string) (string, error) {
	var value string

	item, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return value, storage.KeyNotExistError{}
	}

	if err != nil {
		return value, err
	}

	value = string(item.Value)
	return value, nil
}

// Set stores a value associated with the provided key in the Memcached cache storage.
// It also accepts options which currently only includes TTL (time-to-live) for the key-value pair.
// If the TTL is greater than 0, the key-value pair will be automatically removed from the cache after the TTL duration.
// If the TTL is 0 or less, the key-value pair will persist in the cache until it is manually removed.
func (s *MemcachedCacheStorage) Set(ctx context.Context, key string, value string, options storage.CacheStorageItemOptions) error {
	if options.TTL.Nanoseconds() > 0 {
		err := s.memcache.Set(&memcache.Item{Key: key, Value: []byte(value), Expiration: int32(options.TTL.Seconds())})

		if err != nil {
			return err
		}

		return nil
	}

	err := s.memcache.Set(&memcache.Item{Key: key, Value: []byte(value)})

	if err != nil {
		return err
	}

	return nil
}

// TTL retrieves the time-to-live (TTL) for the provided key from the Memcached cache storage.
// It returns a boolean indicating whether the key has a TTL, the TTL as a time.Duration, and an error if any occurred.
// If the key does not exist, it returns false, a zero duration, and a KeyNotExistError.
// If the key exists but does not have an expiration, it returns false, a zero duration, and nil error.
// If the key exists and has an expiration, it returns true, the TTL, and nil error.
func (s *MemcachedCacheStorage) TTL(ctx context.Context, key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false

	item, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return hasTTL, ttl, storage.KeyNotExistError{}
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
func (s *MemcachedCacheStorage) Del(ctx context.Context, key string) (bool, error) {
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
func (s *MemcachedCacheStorage) GetWithTTL(ctx context.Context, key string) (string, time.Duration, error) {
	value, err := s.Get(ctx, key)

	if err != nil {
		return value, 0, err
	}

	return value, 0, nil
}
