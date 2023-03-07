// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"errors"
	"sync"
	"time"

	"github.com/ksysoev/anycache/storage"
)

const EMPTY_VALUE = ""

// CacheStorage
type CacheStorage[K comparable, V any] interface {
	Get(K) (V, error)
	Set(K, V, storage.CacheStorageItemOptions) error
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
	TTL       time.Duration
	WarmUpTTL time.Duration
}

// NewCache creates instance of Cache
func NewCache[K comparable, V any](storage CacheStorage[K, V]) Cache[K, V] {
	return Cache[K, V]{
		storage:    storage,
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
		if options.WarmUpTTL.Nanoseconds() == 0 {
			return value, nil
		}

		hasTTL, ttl, err := c.storage.TTL(key)

		if err != nil || !hasTTL {
			// something went wrong, lets return already fetched value
			// Also if key doesn't have TTL
			return value, nil
		}

		if ttl.Nanoseconds() > options.WarmUpTTL.Nanoseconds() {
			//Not ready for warm up
			return value, nil
		}

		isLocked, l := c.acquireLock(key, false)

		if !isLocked {
			return value, nil
		}

		defer c.releaseLock(key, l)

		newValue, err := generator()

		if err != nil {
			return value, err
		}

		err = c.storage.Set(key, newValue, storage.CacheStorageItemOptions{TTL: options.TTL})

		if err != nil {
			return value, err
		}

		return newValue, nil

	}

	if !errors.Is(err, storage.KeyNotExistError{}) {
		return value, err
	}

	_, l := c.acquireLock(key, true)
	defer c.releaseLock(key, l)

	value, err = c.storage.Get(key)

	if err == nil {
		return value, nil
	}

	if !errors.Is(err, storage.KeyNotExistError{}) {
		return value, err
	}

	newValue, err := generator()
	if err != nil {
		return value, err
	}

	err = c.storage.Set(key, newValue, storage.CacheStorageItemOptions{TTL: options.TTL})

	if err != nil {
		return value, err
	}

	return newValue, nil
}

func (c *Cache[K, V]) acquireLock(key K, wait bool) (bool, *sync.Mutex) {
	c.globalLock.Lock()
	l, ok := c.locks[key]
	if !ok {
		l = &(sync.Mutex{})
		c.locks[key] = l
	}
	c.globalLock.Unlock()

	if wait {
		l.Lock()
		return true, l
	}

	isLocked := l.TryLock()

	return isLocked, l
}

func (c *Cache[K, V]) releaseLock(key K, l *sync.Mutex) {
	l.Unlock()
	delete(c.locks, key)
}
