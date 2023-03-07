package redis_storage

import (
	"errors"
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

	mock.ExpectGet("testKey1").RedisNil()
	_, err = redisStore.Get("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRedisCacheStorageSet(t *testing.T) {
	redisClient, mock := redismock.NewClientMock()
	redisStore := NewRedisCacheStorage(redisClient)

	mock.ExpectSet("testKey", "testValue", 0).SetVal("OK")

	err := redisStore.Set("testKey", "testValue", storage.CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	mock.ExpectSet("testKey1", "testValue", 1*time.Second).SetVal("OK")
	err = redisStore.Set("testKey1", "testValue", storage.CacheStorageItemOptions{TTL: 1 * time.Second})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if err = mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
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
		t.Errorf("Expected to get TTL as 1000 millisecond, but it has value %v microseconds", ttl.Milliseconds())
	}

	mock.ExpectTTL("testKey1").SetVal(-2 * time.Second)
	_, ttl, err = redisStore.TTL("testKey1")

	if !errors.Is(err, storage.KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", storage.KeyNotExistError{}, err)
	}

	mock.ExpectTTL("testKey2").SetVal(-1 * time.Second)

	hasTTL, ttl, err = redisStore.TTL("testKey2")

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it has")
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
