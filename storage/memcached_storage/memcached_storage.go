package redis_storage

import (
	"context"
	"errors"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache/storage"
	"github.com/redis/go-redis"
)

type MemcachedCacheStorage struct {
	memcached *memcache.Client
}

// NewRedisCacheStorage creates a new RedisCacheStorage
func NewRedisCacheStorage(memcached *memcache.Client) MemcachedCacheStorage {
	return MemcachedCacheStorage{memcached}
}

// Get returns a value from Redis by key
func (s MemcachedCacheStorage) Get(key string) (string, error) {
	var value string

	item, err := s.memcached.Get(context.Background(), key).Result()

	if errors.Is(err, redis.Nil) {
		return value, storage.KeyNotExistError{}
	}

	if err != nil {
		return value, err
	}

	value = item
	return value, nil
}

// Set sets a value in Redis by key
func (s MemcachedCacheStorage) Set(key string, value string, options storage.CacheStorageItemOptions) error {
	if options.TTL.Nanoseconds() > 0 {
		err := s.memcached.Set(context.Background(), key, value, options.TTL).Err()

		if err != nil {
			return err
		}

		return nil
	}

	err := s.memcached.Set(context.Background(), key, value, 0).Err()

	if err != nil {
		return err
	}

	return nil
}

// TTL returns a TTL of a key in Redis
func (s MemcachedCacheStorage) TTL(key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false
	item, err := s.memcached.TTL(context.Background(), key).Result()

	if err != nil {
		return hasTTL, ttl, err
	}

	if item.Nanoseconds() >= 0 {
		return true, item, nil
	}

	if item.Seconds() == -1 {
		return false, item, nil
	}

	if item.Seconds() == -2 {
		return false, item, storage.KeyNotExistError{}
	}

	panic("Unexpected TTL value returned from Redis" + item.String())
}

// Del deletes a key from Redis
func (s MemcachedCacheStorage) Del(key string) (bool, error) {
	deleted, err := s.memcached.Del(context.Background(), key).Result()

	if err != nil {
		return false, err
	}

	isDeleted := deleted > 0

	return isDeleted, nil
}
