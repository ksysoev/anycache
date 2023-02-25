package anycache

import (
	"errors"
	"testing"
)

func TestCache(t *testing.T) {
	cache := NewCache()

	val, err := cache.Cache("testKey", func() (string, error) { return "testValue", nil })

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache("testKey", func() (string, error) { return "testValue1", nil })

	if err != nil {
		t.Errorf("Expected to get no error, but got %v", err)
	}

	if val != "testValue" {
		t.Errorf("Expected to get testValue, but got '%v'", val)
	}

	val, err = cache.Cache("testKey1", func() (string, error) { return "", errors.New("TestError") })

	if err == errors.New("TestError") {
		t.Errorf("Expected to get TestError, but got %v", err)
	}

	if val != "" {
		t.Errorf("Expected to get empty string, but got '%v'", val)
	}
}
