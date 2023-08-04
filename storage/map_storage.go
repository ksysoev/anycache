package storage

import (
	"time"
)

type CacheStorageItemOptions struct {
	TTL time.Duration
}

// MapCacheStorage is a simple map based cache storage
type MapCacheStorage map[string]MapCacheStorageItem

// MapCacheStorageItem
type MapCacheStorageItem struct {
	value     string
	hasTTL    bool
	expiresAt time.Time
}

// NewMapCacheStorage creates instance of MapCacheStorage
func NewMapCacheStorage() MapCacheStorage {
	return MapCacheStorage{}
}

// Get returns value for requested key
func (s MapCacheStorage) Get(key string) (string, error) {
	var value string
	item, ok := s[key]

	if !ok {
		return value, KeyNotExistError{}
	}

	if item.hasTTL && item.expiresAt.Before(time.Now()) {
		_, err := s.Del(key)

		if err != nil {
			return value, err
		}

		return value, KeyNotExistError{}
	}

	value = item.value
	return value, nil
}

// Set saves value into mape storage
func (s MapCacheStorage) Set(key string, value string, options CacheStorageItemOptions) error {

	if options.TTL.Nanoseconds() > 0 {
		expiresAt := time.Now().Add(options.TTL)
		s[key] = MapCacheStorageItem{value: value, expiresAt: expiresAt, hasTTL: true}
		return nil
	}

	s[key] = MapCacheStorageItem{value: value, hasTTL: false}

	return nil
}

// TTL returns time to live for requested key
func (s MapCacheStorage) TTL(key string) (bool, time.Duration, error) {
	var ttl time.Duration
	hasTTL := false

	item, ok := s[key]

	if !ok {
		return hasTTL, ttl, KeyNotExistError{}
	}

	if !item.hasTTL {
		return item.hasTTL, ttl, nil
	}

	if item.expiresAt.Before(time.Now()) {
		_, err := s.Del(key)

		if err != nil {
			return hasTTL, ttl, err
		}

		return hasTTL, ttl, KeyNotExistError{}
	}

	ttl = item.expiresAt.Sub(time.Now())

	return item.hasTTL, ttl, nil
}

// Del deletes key from storage
func (s MapCacheStorage) Del(key string) (bool, error) {
	_, ok := s[key]

	if ok {
		delete(s, key)
		return true, nil
	}

	return false, nil
}
