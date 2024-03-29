package redisstor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ksysoev/anycache/storage"
	"github.com/redis/go-redis/v9"
)

func getRedisOptions() *redis.Options {
	TestRedisHost := os.Getenv("TEST_REDIS_HOST")
	if TestRedisHost == "" {
		TestRedisHost = "localhost"
	}

	TestRedisPort := os.Getenv("TEST_REDIS_PORT")
	if TestRedisPort == "" {
		TestRedisPort = "6379"
	}

	return &redis.Options{Addr: fmt.Sprintf("%s:%s", TestRedisHost, TestRedisPort)}
}

func TestRedisCacheStorageGet(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	ctx := context.Background()

	redisClient.Set(ctx, "TestRedisCacheStorageGetKey", "testValue", 0*time.Second)

	value, err := redisStore.Get(ctx, "TestRedisCacheStorageGetKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if value != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", value)
	}

	_, err = redisStore.Get(ctx, "TestRedisCacheStorageGetKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}
}

func TestRedisCacheStorageSet(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	ctx := context.Background()

	err := redisStore.Set(ctx, "TestRedisCacheStorageSetKey", "testValue", storage.CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val, _ := redisClient.Get(ctx, "TestRedisCacheStorageSetKey").Result()

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	err = redisStore.Set(ctx, "TestRedisCacheStorageSetKey1", "testValue", storage.CacheStorageItemOptions{TTL: 2 * time.Second})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val1, _ := redisClient.Get(ctx, "TestRedisCacheStorageSetKey1").Result()

	if val1 != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val1)
	}

	ttl, _ := redisClient.TTL(ctx, "TestRedisCacheStorageSetKey1").Result()

	if ttl.Milliseconds() <= 0 || ttl.Milliseconds() > 2000 {
		t.Errorf("Expected to get valid TTL, but it has value %v", ttl.Milliseconds())
	}
}

func TestRedisCacheStorageTTL(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	ctx := context.Background()

	redisClient.Set(ctx, "TestRedisCacheStorageTTLKey", "testValue", 1*time.Second)

	hasTTL, ttl, err := redisStore.TTL(ctx, "TestRedisCacheStorageTTLKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Milliseconds() < 0 || ttl.Milliseconds() > 1000 {
		t.Errorf("Expected to get TTL as 1000 millisecond, but it has value %v microseconds", ttl.Milliseconds())
	}

	_, _, err = redisStore.TTL(ctx, "TestRedisCacheStorageTTLKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	redisClient.Set(ctx, "TestRedisCacheStorageTTLKey2", "testValue", 0*time.Second)
	hasTTL, _, err = redisStore.TTL(ctx, "TestRedisCacheStorageTTLKey2")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it has")
	}
}

func TestRedisCacheStorageDel(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	ctx := context.Background()

	redisClient.Set(ctx, "TestRedisCacheStorageDelKey", "testValue", 0*time.Second)

	_, err := redisStore.Del(ctx, "TestRedisCacheStorageDelKey")
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	_, err = redisClient.Get(ctx, "TestRedisCacheStorageDelKey").Result()

	if !errors.Is(err, redis.Nil) {
		t.Errorf("Expected to get error %v, but got '%v'", redis.Nil, err)
	}
}
