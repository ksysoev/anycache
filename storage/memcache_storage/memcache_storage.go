package memcache_storage

import (
	"errors"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache/storage"
)

type MemcachedCacheStorage struct {
	memcache *memcache.Client
}

// NewRedisCacheStorage creates a new RedisCacheStorage
func NewMemcachedCacheStorage(memcache *memcache.Client) MemcachedCacheStorage {
	return MemcachedCacheStorage{memcache}
}

// Get returns a value from Redis by key
func (s MemcachedCacheStorage) Get(key string) (string, error) {
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

// Set sets a value in Redis by key
func (s MemcachedCacheStorage) Set(key string, value string, options storage.CacheStorageItemOptions) error {
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

// TTL returns a TTL of a key in Redis
func (s MemcachedCacheStorage) TTL(key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false

	item, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return hasTTL, -2, nil
	}

	if err != nil {
		return hasTTL, ttl, err
	}

	if item.Expiration > 0 {
		return true, time.Duration(item.Expiration) * time.Second, nil
	}

	return false, 0, nil
}

// Del deletes a key from Redis
func (s MemcachedCacheStorage) Del(key string) (bool, error) {
	err := s.memcache.Delete(key)

	if err != nil {
		return false, err
	}

	return true, nil
}
