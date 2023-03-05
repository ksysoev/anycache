package redis_storage

import (
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/ksysoev/anycache/storage"
)

func TestRedisCacheStorageGet(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	storage := NewRedisCacheStorage(redisClient)

	mock.ExpectGet("testKey").SetVal("testValue")

	value, err := storage.Get("testKey")

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
	storage := NewRedisCacheStorage(redisClient)

	mock.ExpectSet("testKey", "testValue", 0)

	storage.Set("testKey", "testValue", storage.CacheStorageItemOptions{})

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageTTL(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	storage := NewRedisCacheStorage(redisClient)

	mock.ExpectTTL("testKey").SetVal(1 * time.Second)

	hasTTL, ttl, err := storage.TTL("testKey")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Microseconds() <= 0 || ttl.Microseconds() >= 1000 {
		t.Errorf("Expected to have TTL less than start value, but it has value %v", ttl.Microseconds())
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageDel(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	storage := NewRedisCacheStorage(redisClient)

	mock.ExpectDel("testKey").SetVal(1)

	storage.Del("testKey")

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
