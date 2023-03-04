package redis_storage

import (
	"context"
	"time"

	"github.com/ksysoev/anycache/storage"
	"github.com/redis/go-redis/v9"
)

type RedisCacheStorage struct {
	redisDB redis.Client
}

func NewRedisCacheStorage(redisDB redis.Client) RedisCacheStorage {
	return RedisCacheStorage{redisDB: redisDB}
}

func (s RedisCacheStorage) Get(key string) (string, error) {
	var value string

	item, err := s.redisDB.Get(context.Background(), key).Result()

	if err != nil {
		return value, err
	}

	value = item
	return value, nil
}

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

func (s RedisCacheStorage) TTL(key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false
	item, err := s.redisDB.TTL(context.Background(), key).Result()

	if err != nil {
		return hasTTL, ttl, err
	}

	if item.Nanoseconds() > 0 {
		hasTTL = true
		ttl = item
	}

	return hasTTL, ttl, nil
}

func (s RedisCacheStorage) Del(key string) (bool, error) {
	deleted, err := s.redisDB.Del(context.Background(), key).Result()

	if err != nil {
		return false, err
	}

	isDeleted := deleted > 0

	return isDeleted, nil
}
