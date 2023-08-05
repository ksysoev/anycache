// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"errors"
	"math/rand"
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
	key       string
	value     string
	err       error
	warmingUp bool
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

type CacheQueue struct {
	requests     []CacheReuest
	WarmingUp    bool
	currentValue string
}

// NewCache creates instance of Cache
func NewCache(storage CacheStorage, options CacheOptions) Cache {
	c := Cache{
		Storage:      storage,
		randomizeTTL: options.randomizeTTL,
		requests:     make(chan CacheReuest),
		responses:    make(chan CacheResponse),
	}

	go c.requestHandler()

	return c
}

func (c *Cache) Cache(key string, generator func() (string, error), options CacheItemOptions) (string, error) {
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
	requestStorage := map[string]CacheQueue{}

	for {
		select {
		case req := <-c.requests:
			reqQ, ok := requestStorage[req.key]

			if ok {
				if reqQ.WarmingUp {
					req.response <- CacheResponse{
						key:   req.key,
						value: reqQ.currentValue,
						err:   nil,
					}
					continue
				}
				reqQ.requests = append(reqQ.requests, req)
				requestStorage[req.key] = reqQ
				continue
			}

			requestStorage[req.key] = CacheQueue{requests: []CacheReuest{req}}
			go c.processRequest(req.key, req.generator, req.options)

		case resp := <-c.responses:

			reqQ, ok := requestStorage[resp.key]

			if !ok {
				continue
			}

			if resp.warmingUp {
				reqQ.currentValue = resp.value
				reqQ.WarmingUp = true
				processNow := reqQ.requests[1:]
				reqQ.requests = reqQ.requests[:1]
				requestStorage[resp.key] = reqQ

				for _, req := range processNow {
					req.response <- resp
				}

				continue

			}

			for _, req := range reqQ.requests {
				req.response <- resp
			}

			delete(requestStorage, resp.key)
		}
	}
}

// processRequest processes a cache request with the given key, generator function, and options.
// If the cache storage has a value for the key, it returns the value and any error encountered.
// If the cache storage does not have a value for the key, it generates a value using the provided generator function,
// sets it in the cache storage with the given key and options, and returns the generated value and any error encountered.
// If the cache item options include a warm-up TTL, it checks if the current TTL of the key is less than or equal to the warm-up TTL.
// If the current TTL is less than or equal to the warm-up TTL, it sets the value in the cache storage again with the same key and options,
// and returns the new value and any error encountered.
func (c *Cache) processRequest(key string, generator func() (string, error), options CacheItemOptions) {
	var value string
	var err error
	var needWarmUp bool
	if options.WarmUpTTL.Nanoseconds() > 0 {
		var ttl time.Duration
		value, ttl, err = c.GetWithTTL(key)

		readyForWarmUp := ttl.Nanoseconds() != 0 && ttl.Nanoseconds() <= options.WarmUpTTL.Nanoseconds()
		if err == nil && readyForWarmUp {
			needWarmUp = true
		}
	} else {
		value, err = c.Storage.Get(key)
	}

	if err != nil && errors.Is(err, storage.KeyNotExistError{}) {
		value, err = c.generateAndSet(key, generator, options)
	}

	cacheResp := CacheResponse{
		key:       key,
		value:     value,
		err:       err,
		warmingUp: needWarmUp,
	}

	c.responses <- cacheResp

	if needWarmUp {
		newVal, err := c.generateAndSet(key, generator, options)

		cacheResp := CacheResponse{
			key:       key,
			value:     newVal,
			err:       err,
			warmingUp: false,
		}

		c.responses <- cacheResp
	}
}

func (c *Cache) GetWithTTL(key string) (string, time.Duration, error) {
	value, err := c.Storage.Get(key)

	if err != nil {
		return value, 0, err
	}

	hasTTL, ttl, err := c.Storage.TTL(key)

	if err != nil {
		return value, 0, err
	}

	if !hasTTL {
		return value, 0, nil
	}

	return value, ttl, nil
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
