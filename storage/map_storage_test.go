package storage

import (
	"errors"
	"testing"
	"time"
)

func TestMapCacheStorageGetSet(t *testing.T) {
	storage := MapCacheStorage[string, string]{}

	testKey := "testKey"
	testValue := "testValue"

	_, err := storage.Get(testKey)

	if !errors.Is(err, KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", KeyNotExistError{}, err)
	}

	err = storage.Set(testKey, testValue, CacheStorageItemOptions{})
	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val, err := storage.Get(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != testValue {
		t.Errorf("Expected to get %v, but got %v", testValue, val)
	}
}

func TestMapCacheStorageTTL(t *testing.T) {
	testKey := "testKey"
	testValue := "testValue"

	storage := MapCacheStorage[string, string]{}

	err := storage.Set(testKey, testValue, CacheStorageItemOptions{TTL: time.Millisecond})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	hasTTL, ttl, err := storage.TTL(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !hasTTL {
		t.Errorf("Expected to have TTL, but it doesnt")
	}

	if ttl.Microseconds() <= 0 || ttl.Microseconds() >= 1000 {
		t.Errorf("Expected to have TTL less than start value, but it has value %v", ttl.Microseconds())
	}

	val, err := storage.Get(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != testValue {
		t.Errorf("Expected to get %v, but got %v", testValue, val)
	}

	time.Sleep(time.Millisecond)
	val, err = storage.Get(testKey)

	if !errors.Is(err, KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v' %v", KeyNotExistError{}, err, val)
	}

	_ = storage.Set(testKey, testValue, CacheStorageItemOptions{})
	hasTTL, _, err = storage.TTL(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if hasTTL {
		t.Errorf("Expected to have no TTL, but it does")
	}

}

func TestMapCacheStorageDel(t *testing.T) {
	testKey := "testKey"
	testValue := "testValue"

	storage := MapCacheStorage[string, string]{}

	err := storage.Set(testKey, testValue, CacheStorageItemOptions{})

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	val, err := storage.Get(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != testValue {
		t.Errorf("Expected to get %v, but got %v", testValue, val)
	}

	isDeleted, err := storage.Del(testKey)

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if !isDeleted {
		t.Errorf("Expected to be deleted, but it is not")
	}
}
