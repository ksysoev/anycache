package anycache

import (
	"errors"
	"testing"
)

func TestMapCacheStorageGetSet(t *testing.T) {
	storage := MapCacheStorage[string, string]{}

	testKey := "testKey"
	testValue := "testValue"

	_, err := storage.Get(testKey)

	if !errors.Is(err, KeyNotExistError{}) {
		t.Errorf("Expected to get error %v, but got '%v'", KeyNotExistError{}, err)
	}

	err = storage.Set(testKey, testValue)
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