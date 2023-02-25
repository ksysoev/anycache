package anycache

import (
	"errors"
	"testing"
	"time"
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

func TestCacheConcurrency(t *testing.T) {
	cache := NewCache()

	results := make(chan string)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue", nil
		})
		ch <- val
	}(&cache, results)

	go func(c *Cache, ch chan string) {
		val, _ := c.Cache("testKey", func() (string, error) {
			time.Sleep(time.Millisecond)
			return "testValue1", nil
		})
		ch <- val
	}(&cache, results)

	val1, val2 := <-results, <-results

	if val1 != "testValue" && val1 != "testValue1" {
		t.Errorf("Expected to get testValue as a result, but got '%v'", val1)
	}

	if val1 != val2 {
		t.Errorf("Expected to get same result for concurent requests, but got '%v' and '%v", val1, val2)
	}
}
