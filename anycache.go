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
	globalLock   *sync.Mutex
	locks        map[string]*sync.Mutex
	requests     chan CacheReuest
	responses    chan CacheResponse
}

type CacheReuest struct {
	key       string
	generator func() (string, error)
	options   CacheItemOptions
	response  chan CacheResponse
}

type CacheResponse struct {
	key   string
	value string
	err   error
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
	c := Cache{
		Storage:      storage,
		randomizeTTL: options.randomizeTTL,
		globalLock:   &sync.Mutex{},
		locks:        map[string]*sync.Mutex{},
		requests:     make(chan CacheReuest),
		responses:    make(chan CacheResponse),
	}

	go c.requestHandler()

	return c
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

func (c *Cache) CacheAsync(key string, generator func() (string, error), options CacheItemOptions) (string, error) {
	response := make(chan CacheResponse)

	c.requests <- CacheReuest{
		key:       key,
		generator: generator,
		options:   options,
		response:  response,
	}

	resp := <-response

	return resp.value, resp.err
}

func (c *Cache) requestHandler() {
	requestStorage := map[string][]CacheReuest{}

	for {
		select {
		case req := <-c.requests:
			reqQ, ok := requestStorage[req.key]

			if ok {
				requestStorage[req.key] = append(reqQ, req)
				continue
			}

			requestStorage[req.key] = []CacheReuest{req}
			go func(key string, generator func() (string, error), options CacheItemOptions) {
				value, err := c.Storage.Get(key)

				if err != nil && errors.Is(err, storage.KeyNotExistError{}) {
					value, err = c.generateAndSet(key, generator, options)
				}

				cacheResp := CacheResponse{
					key:   key,
					value: value,
					err:   err,
				}

				c.responses <- cacheResp

			}(req.key, req.generator, req.options)

		case resp := <-c.responses:

			reqQ, ok := requestStorage[resp.key]

			if !ok {
				continue
			}

			for _, req := range reqQ {
				req.response <- resp
			}

			delete(requestStorage, resp.key)
		}
	}
}

// acquireLock acquires a lock for the given key. If the lock is already held by another goroutine, it will either wait or return immediately depending on the value of the wait parameter.
// Returns a boolean indicating whether the lock was acquired and a pointer to the lock.
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

// releaseLock releases the lock for the given key.
// It unlocks the mutex and removes it from the locks map.
func (c *Cache) releaseLock(key string, l *sync.Mutex) {
	l.Unlock()
	delete(c.locks, key)
}

// generateAndSet generates a value using the provided generator function,
// sets it in the cache storage with the given key and options,
// and returns the generated value and any error encountered.
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

// randomizeTTL randomizes the TTL (time-to-live) duration by a percentage defined by persentOfRandomTTL constant.
// It takes a time.Duration as input and returns a time.Duration as output.
// If the input duration is zero, it returns the same duration.
func randomizeTTL(ttl time.Duration) time.Duration {
	if ttl.Nanoseconds() == 0 {
		return ttl
	}

	MaxShift := float64(ttl.Nanoseconds()) * persentOfRandomTTL / 100.0
	randomizedShift := int64((MaxShift * (float64(rand.Intn(100)) - 50.0) / 100.0))

	randomizedTTL := ttl.Nanoseconds() + randomizedShift
	return time.Duration(randomizedTTL)
}
