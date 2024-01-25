package redisstor

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
func NewRedisCacheStorage(redisDB *redis.Client) *RedisCacheStorage {
	return &RedisCacheStorage{redisDB: redisDB}
}

// Get retrieves the value associated with the provided key from the Redis cache storage.
// It returns the value as a string and an error if any occurred.
// If the key does not exist, it returns an empty string and a KeyNotExistError.
// If any other error occurs during the operation, it returns an empty string and the error.
func (s *RedisCacheStorage) Get(ctx context.Context, key string) (string, error) {
	var value string

	item, err := s.redisDB.Get(ctx, key).Result()

	if errors.Is(err, redis.Nil) {
		return value, storage.KeyNotExistError{}
	}

	if err != nil {
		return value, err
	}

	value = item
	return value, nil
}

// Set stores a value associated with the provided key in the Redis cache storage.
// It also accepts options which currently only includes TTL (time-to-live) for the key-value pair.
// If the TTL is greater than 0, the key-value pair will be automatically removed from the cache after the TTL duration.
// If the TTL is 0 or less, the key-value pair will persist in the cache until it is manually removed.
// If an error occurs during the operation, it returns the error.
func (s *RedisCacheStorage) Set(ctx context.Context, key string, value string, options storage.CacheStorageItemOptions) error {
	if options.TTL.Nanoseconds() > 0 {
		err := s.redisDB.Set(ctx, key, value, options.TTL).Err()

		if err != nil {
			return err
		}

		return nil
	}

	err := s.redisDB.Set(ctx, key, value, 0).Err()

	if err != nil {
		return err
	}

	return nil
}

// Del deletes the value associated with the provided key from the Redis cache storage.
// It returns a boolean indicating whether the deletion was successful, and an error if any occurred.
// If the key does not exist, it returns false and nil error.
// If any other error occurs during the operation, it returns false and the error.
func (s *RedisCacheStorage) TTL(ctx context.Context, key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false
	item, err := s.redisDB.PTTL(ctx, key).Result()

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

// Del deletes the value associated with the provided key from the Redis cache storage.
// It returns a boolean indicating whether the deletion was successful, and an error if any occurred.
// If the key does not exist, it returns false and nil error.
// If any other error occurs during the operation, it returns false and the error.
func (s *RedisCacheStorage) Del(ctx context.Context, key string) (bool, error) {
	deleted, err := s.redisDB.Del(ctx, key).Result()

	if err != nil {
		return false, err
	}

	isDeleted := deleted > 0

	return isDeleted, nil
}

// GetWithTTL retrieves the value and time-to-live (TTL) associated with the provided key from the Redis cache storage.
// It returns the value as a string, the TTL as a time.Duration, and an error if any occurred.
// If the key does not exist, it returns an empty string, a zero duration, and a KeyNotExistError.
// If the key exists but does not have an expiration, it returns the value, a zero duration, and nil error.
// If the key exists and has an expiration, it returns the value, the TTL, and nil error.
func (s *RedisCacheStorage) GetWithTTL(ctx context.Context, key string) (string, time.Duration, error) {
	value, err := s.Get(ctx, key)

	if err != nil {
		return value, 0, err
	}

	hasTTL, ttl, err := s.TTL(ctx, key)

	if err != nil {
		return value, 0, err
	}

	if !hasTTL {
		return value, 0, nil
	}

	return value, ttl, nil
}

// Close closes the Redis cache storage connection.
func (s *RedisCacheStorage) Close() error {
	return s.redisDB.Close()
}
