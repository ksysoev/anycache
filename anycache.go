// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/ksysoev/anycache/storage"
)

const persentOfRandomTTL = 10.0

// CacheStorage
type CacheStorage interface {
	Get(string) (string, error)
	Set(string, string, storage.CacheStorageItemOptions) error
	TTL(string) (bool, time.Duration, error)
	Del(string) (bool, error)
}

// Cache
type Cache struct {
	Storage      CacheStorage
	randomizeTTL bool
	globalLock   sync.Mutex
	locks        map[string]*sync.Mutex
}

// CacheOptions
type CacheOptions struct {
	randomizeTTL bool
}

// CacheItemOptions
type CacheItemOptions struct {
	TTL       time.Duration
	WarmUpTTL time.Duration
}

// NewCache creates instance of Cache
func NewCache(storage CacheStorage, options CacheOptions) Cache {
	return Cache{
		Storage:      storage,
		randomizeTTL: options.randomizeTTL,
		globalLock:   sync.Mutex{},
		locks:        map[string]*sync.Mutex{},
	}
}

// Cache trying to retrive value from cache if it exists.
// If not it runs generator function to get the value and saves the value into cache storage
// returns requested value
func (c *Cache) Cache(key string, generator func() (string, error), options CacheItemOptions) (string, error) {
	value, err := c.Storage.Get(key)

	if err == nil {
		if options.WarmUpTTL.Nanoseconds() == 0 {
			return value, nil
		}

		hasTTL, ttl, err := c.Storage.TTL(key)

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

		return c.generateAndSet(key, generator, options)
	}

	if !errors.Is(err, storage.KeyNotExistError{}) {
		return value, err
	}

	_, l := c.acquireLock(key, true)
	defer c.releaseLock(key, l)

	value, err = c.Storage.Get(key)

	if err == nil {
		return value, nil
	}

	if !errors.Is(err, storage.KeyNotExistError{}) {
		return value, err
	}

	return c.generateAndSet(key, generator, options)
}

func (c *Cache) acquireLock(key string, wait bool) (bool, *sync.Mutex) {
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

func (c *Cache) releaseLock(key string, l *sync.Mutex) {
	l.Unlock()
	delete(c.locks, key)
}

func (c *Cache) generateAndSet(key string, generator func() (string, error), options CacheItemOptions) (string, error) {
	value, err := generator()

	if err != nil {
		return value, err
	}

	ttl := options.TTL

	if c.randomizeTTL {
		ttl = randomizeTTL(options.TTL)
	}

	err = c.Storage.Set(key, value, storage.CacheStorageItemOptions{TTL: ttl})

	if err != nil {
		return value, err
	}

	return value, nil
}

// randomizeTTL randomize TTL to avoid cache stampede
func randomizeTTL(ttl time.Duration) time.Duration {
	if ttl.Nanoseconds() == 0 {
		return ttl
	}

	MaxShift := float64(ttl.Nanoseconds()) * persentOfRandomTTL / 100.0
	randomizedShift := int64((MaxShift * (float64(rand.Intn(100)) - 50.0) / 100.0))

	randomizedTTL := ttl.Nanoseconds() + randomizedShift
	return time.Duration(randomizedTTL)
}
