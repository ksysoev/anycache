package redis_storage

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

	return &redis.Options{Addr: fmt.Sprintf("%s:%s", TestRedisHost, TestRedisPort), DB: 2}
}

func TestRedisCacheStorageGet(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	ctx := context.Background()

	redisClient.Set(ctx, "testKey", "testValue", 0*time.Second)

	value, err := redisStore.Get("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if value != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", value)
	}

	_, err = redisStore.Get("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	redisClient.FlushDB(ctx)
}

func TestRedisCacheStorageSet(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	err := redisStore.Set("testKey", "testValue", storage.CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val, _ := redisClient.Get(context.Background(), "testKey").Result()

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	err = redisStore.Set("testKey1", "testValue", storage.CacheStorageItemOptions{TTL: 2 * time.Second})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val1, _ := redisClient.Get(context.Background(), "testKey1").Result()

	if val1 != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val1)
	}

	ttl, _ := redisClient.TTL(context.Background(), "testKey1").Result()

	if ttl.Milliseconds() <= 0 || ttl.Milliseconds() > 2000 {
		t.Errorf("Expected to get valid TTL, but it has value %v", ttl.Milliseconds())
	}

	redisClient.FlushDB(context.Background())
}

func TestRedisCacheStorageTTL(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	redisClient.Set(context.Background(), "testKey", "testValue", 1*time.Second)

	hasTTL, ttl, err := redisStore.TTL("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Milliseconds() < 0 || ttl.Milliseconds() > 1000 {
		t.Errorf("Expected to get TTL as 1000 millisecond, but it has value %v microseconds", ttl.Milliseconds())
	}

	_, _, err = redisStore.TTL("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	redisClient.Set(context.Background(), "testKey2", "testValue", 0*time.Second)
	hasTTL, _, err = redisStore.TTL("testKey2")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it has")
	}

	redisClient.FlushDB(context.Background())
}

func TestRedisCacheStorageDel(t *testing.T) {
	redisClient := redis.NewClient(getRedisOptions())
	redisStore := NewRedisCacheStorage(redisClient)

	redisClient.Set(context.Background(), "testKey", "testValue", 0*time.Second)

	redisStore.Del("testKey")

	_, err := redisClient.Get(context.Background(), "testKey").Result()

	if !errors.Is(err, redis.Nil) {
		t.Errorf("Expected to get error %v, but got '%v'", redis.Nil, err)
	}

	redisClient.FlushDB(context.Background())
}
