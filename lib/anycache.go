// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"errors"
	"sync"
	"time"
)

const NOT_EXISTEN_KEY_TTL = -2
const NO_EXPIRATION_KEY_TTL = -1

const EMPTY_VALUE = ""

// CacheStorage
type CacheStorage[K comparable, V any] interface {
	Get(K) (V, error)
	Set(K, V, CacheItemOptions) error
	TTL(K) (bool, time.Duration, error)
	Del(K) (bool, error)
}

// Cache
type Cache[K comparable, V any] struct {
	storage    CacheStorage[K, V]
	globalLock sync.Mutex
	locks      map[K]*sync.Mutex
}

// CacheItemOptions
type CacheItemOptions struct {
	ttl       time.Duration
	warmUpTTL time.Duration
}

// NewCache creates instance of Cache
func NewCache[K comparable, V any]() Cache[K, V] {
	return Cache[K, V]{
		storage:    MapCacheStorage[K, V]{},
		globalLock: sync.Mutex{},
		locks:      map[K]*sync.Mutex{},
	}
}

// Cache trying to retrive value from cache if it exists.
// If not it runs generator function to get the value and saves the value into cache storage
// returns requested value
func (c *Cache[K, V]) Cache(key K, generator func() (V, error), options CacheItemOptions) (V, error) {
	value, err := c.storage.Get(key)

	if err == nil {
		if options.warmUpTTL.Nanoseconds() == 0 {
			return value, nil
		}

		hasTTL, ttl, err := c.storage.TTL(key)

		if err != nil || !hasTTL {
			// something went wrong, lets return already fetched value
			// Also if key doesn't have TTL
			return value, nil
		}

		if ttl.Nanoseconds() > options.warmUpTTL.Nanoseconds() {
			//Not ready for warm up
			return value, nil
		}

		c.globalLock.Lock()
		l, ok := c.locks[key]
		if !ok {
			l = &(sync.Mutex{})
			c.locks[key] = l
		}
		c.globalLock.Unlock()

		isLocked := l.TryLock()

		if !isLocked {
			return value, nil
		}

		newValue, err := generator()

		if err != nil {
			return value, err
		}

		err = c.storage.Set(key, newValue, options)

		if err != nil {
			return value, err
		}

		return newValue, nil

	}

	if !errors.Is(err, KeyNotExistError{}) {
		return value, err
	}

	c.globalLock.Lock()
	l, ok := c.locks[key]
	if !ok {
		l = &(sync.Mutex{})
		c.locks[key] = l
	}
	c.globalLock.Unlock()

	l.Lock()
	defer func() {
		l.Unlock()
		delete(c.locks, key)
	}()

	value, err = c.storage.Get(key)

	if err == nil {
		return value, nil
	}

	if !errors.Is(err, KeyNotExistError{}) {
		return value, err
	}

	newValue, err := generator()

	if err != nil {
		return value, err
	}

	err = c.storage.Set(key, newValue, options)

	if err != nil {
		return value, err
	}

	return newValue, nil
}
