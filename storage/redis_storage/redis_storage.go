package redis_storage

import (
	"context"
	"errors"
	"time"

	"github.com/ksysoev/anycache/storage"
	"github.com/redis/go-redis/v9"
)

type RedisCacheStorage struct {
	redisDB *redis.Client
}

// NewRedisCacheStorage creates a new RedisCacheStorage
func NewRedisCacheStorage(redisDB *redis.Client) RedisCacheStorage {
	return RedisCacheStorage{redisDB: redisDB}
}

// Get returns a value from Redis by key
func (s RedisCacheStorage) Get(key string) (string, error) {
	var value string

	item, err := s.redisDB.Get(context.Background(), key).Result()

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
func (s RedisCacheStorage) Set(key string, value string, options storage.CacheStorageItemOptions) error {
	if options.TTL.Nanoseconds() > 0 {
		err := s.redisDB.Set(context.Background(), key, value, options.TTL).Err()

		if err != nil {
			return err
		}

		return nil
	}

	err := s.redisDB.Set(context.Background(), key, value, 0).Err()

	if err != nil {
		return err
	}

	return nil
}

// TTL returns a TTL of a key in Redis
func (s RedisCacheStorage) TTL(key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false
	item, err := s.redisDB.PTTL(context.Background(), key).Result()

	if err != nil {
		return hasTTL, ttl, err
	}

	if item.Nanoseconds() >= 0 {
		return true, item, nil
	}

	if item.Nanoseconds() == -1 {
		return false, item, nil
	}

	if item.Nanoseconds() == -2 {
		return false, item, storage.KeyNotExistError{}
	}

	return hasTTL, 0 * time.Second, errors.New("Unexpected TTL value returned from Redis" + item.String())
}

// Del deletes a key from Redis
func (s RedisCacheStorage) Del(key string) (bool, error) {
	deleted, err := s.redisDB.Del(context.Background(), key).Result()

	if err != nil {
		return false, err
	}

	isDeleted := deleted > 0

	return isDeleted, nil
}
