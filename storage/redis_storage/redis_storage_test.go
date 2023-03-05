package redis_storage

import (
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/ksysoev/anycache/storage"
)

func TestRedisCacheStorageGet(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	redisStore := NewRedisCacheStorage(redisClient)

	mock.ExpectGet("testKey").SetVal("testValue")

	value, err := redisStore.Get("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if value != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", value)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageSet(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	redisStore := NewRedisCacheStorage(redisClient)

	mock.ExpectSet("testKey", "testValue", 0)

	redisStore.Set("testKey", "testValue", storage.CacheStorageItemOptions{})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageTTL(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	redisStore := NewRedisCacheStorage(redisClient)

	mock.ExpectTTL("testKey").SetVal(1 * time.Second)

	hasTTL, ttl, err := redisStore.TTL("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Milliseconds() != 1000 {
		t.Errorf("Expected to get TTL as 1000 milisecond, but it has value %v microseconds", ttl.Milliseconds())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageDel(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	redisStore := NewRedisCacheStorage(redisClient)

	mock.ExpectDel("testKey").SetVal(1)

	redisStore.Del("testKey")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
