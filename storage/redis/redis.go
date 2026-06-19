package redis

import (
	"context"
	"errors"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/redis/go-redis/v9"
)

type Storage struct {
	redisDB Client
}

type Client interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	PTTL(ctx context.Context, key string) *redis.DurationCmd
}

// New creates a new RedisCacheStorage
func New(redisDB Client) *Storage {
	return &Storage{redisDB: redisDB}
}

// Get retrieves the value associated with the provided key from the Redis cache storage.
// It returns the value as a byte array and an error if any occurred.
// If the key does not exist, it returns a nil and a ErrKeyNotExists.
// If any other error occurs during the operation, it returns a nil and the error.
func (s *Storage) Get(ctx context.Context, key string) ([]byte, error) {
	item, err := s.redisDB.Get(ctx, key).Bytes()

	if errors.Is(err, redis.Nil) {
		return nil, anycache.ErrKeyNotExists
	}

	if err != nil {
		return nil, err
	}

	return item, nil
}

// Set stores a value associated with the provided key in the Redis cache storage.
// It also accepts options which currently only includes TTL (time-to-live) for the key-value pair.
// If the TTL is greater than 0, the key-value pair will be automatically removed from the cache after the TTL duration.
// If the TTL is 0 or less, the key-value pair will persist in the cache until it is manually removed.
// If an error occurs during the operation, it returns the error.
func (s *Storage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl > 0 {
		err := s.redisDB.Set(ctx, key, value, ttl).Err()
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

// ttl retrieves the time-to-live (TTL) for the provided key from the Redis cache storage.
func (s *Storage) ttl(ctx context.Context, key string) (bool, time.Duration, error) {
	item, err := s.redisDB.PTTL(ctx, key).Result()
	if err != nil {
		return false, time.Duration(0), err
	}

	if item == time.Duration(-1) {
		return false, item, nil
	}

	if item == time.Duration(-2) {
		return false, item, anycache.ErrKeyNotExists
	}

	if item >= 0 {
		return true, item, nil
	}

	return false, time.Duration(0), errors.New("unexpected TTL value returned from Redis: " + item.String())
}

// Del deletes the value associated with the provided key from the Redis cache storage.
// It returns an error if any occurred.
func (s *Storage) Del(ctx context.Context, key string) error {
	_, err := s.redisDB.Del(ctx, key).Result()

	return err
}

// GetWithTTL retrieves the value and time-to-live (TTL) associated with the provided key from the Redis cache storage.
// It returns the value as a byte array, the TTL as a time.Duration, and an error if any occurred.
// If the key does not exist, it returns a nil, a zero duration, and a ErrKeyNotExists.
// If the key exists but does not have an expiration, it returns the value, a zero duration, and nil error.
// If the key exists and has an expiration, it returns the value, the TTL, and nil error.
func (s *Storage) GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return value, 0, err
	}

	hasTTL, ttl, err := s.ttl(ctx, key)
	if err != nil {
		return value, 0, err
	}

	if !hasTTL {
		return value, 0, nil
	}

	return value, ttl, nil
}
