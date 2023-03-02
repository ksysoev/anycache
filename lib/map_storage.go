package anycache

import (
	"time"
)

type MapCacheStorage[K comparable, V any] map[K]MapCacheStorageItem[V]

type MapCacheStorageItem[V any] struct {
	value     V
	hasTTL    bool
	expiresAt time.Time
}

func NewMapCacheStorage[K comparable, V any]() MapCacheStorage[K, V] {
	return MapCacheStorage[K, V]{}
}

// Get returns value for requested key
func (s MapCacheStorage[K, V]) Get(key K) (V, error) {
	var value V
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
func (s MapCacheStorage[K, V]) Set(key K, value V, options CacheItemOptions) error {

	if options.ttl.Nanoseconds() > 0 {
		expiresAt := time.Now().Add(options.ttl)
		s[key] = MapCacheStorageItem[V]{value: value, expiresAt: expiresAt, hasTTL: true}
		return nil
	}

	s[key] = MapCacheStorageItem[V]{value: value, hasTTL: false}

	return nil
}

// TTL returns time to live for requested key
func (s MapCacheStorage[K, V]) TTL(key K) (bool, time.Duration, error) {
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

// Del deletes key from map storage
func (s MapCacheStorage[K, V]) Del(key K) (bool, error) {
	_, ok := s[key]

	if ok {
		delete(s, key)
		return true, nil
	}

	return false, nil
}
