// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"time"

	"github.com/ksysoev/anycache/storage"
)

// CacheStorage
type CacheStorage interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, storage.CacheStorageItemOptions) error
	TTL(context.Context, string) (bool, time.Duration, error)
	Del(context.Context, string) (bool, error)
	GetWithTTL(context.Context, string) (string, time.Duration, error)
}

// Cache
type Cache struct {
	Storage     CacheStorage
	maxShiftTTL uint8 // max shift of TTL in persent
	requests    chan *CacheReuest
	responses   chan CacheResponse
	cancel      chan *CacheReuest
}

type CacheReuest struct {
	key       string
	generator CacheGenerator
	TTL       time.Duration
	WarmUpTTL time.Duration
	response  chan CacheResponse
	ctx       context.Context
}

type CacheResponse struct {
	key       string
	value     string
	err       error
	warmingUp bool
}

type CacheQueue struct {
	requests     []*CacheReuest
	WarmingUp    bool
	cancelCtx    context.CancelFunc
	currentValue string
}

type CacheGenerator func(ctx context.Context) (string, error)
type CacheOptions func(*Cache)

type CacheItemOptions func(*CacheReuest)

// NewCache creates a new Cache instance with the provided CacheStorage and CacheOptions.
// WithTTLRandomization sets max shift of TTL in persent
// It returns the created Cache instance.
func NewCache(storage CacheStorage, opts ...CacheOptions) Cache {
	c := Cache{
		Storage:   storage,
		requests:  make(chan *CacheReuest),
		responses: make(chan CacheResponse),
		cancel:    make(chan *CacheReuest),
	}

	for _, opt := range opts {
		opt(&c)
	}

	go c.requestHandler()

	return c
}

// WithTTLRandomization sets max shift of TTL in persent
func WithTTLRandomization(maxShiftPercent uint8) func(*Cache) {
	return func(c *Cache) {
		c.maxShiftTTL = maxShiftPercent
	}
}

// WithTTL sets TTL for cache item
func WithTTL(ttl time.Duration) CacheItemOptions {
	return func(req *CacheReuest) {
		req.TTL = ttl
	}
}

// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func WithWarmUpTTL(ttl time.Duration) CacheItemOptions {
	return func(req *CacheReuest) {
		req.WarmUpTTL = ttl
	}
}

func WithCtx(ctx context.Context) CacheItemOptions {
	return func(req *CacheReuest) {
		req.ctx = ctx
	}
}

// Cache caches the result of the generator function for the given key.
// If the key already exists in the cache, the cached value is returned.
// Otherwise, the generator function is called to generate a new value,
// which is then cached and returned.
// The function takes an optional list of CacheItemOptions to customize the caching behavior.
// WithTTL sets TTL for cache item
// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func (c *Cache) Cache(key string, generator CacheGenerator, opts ...CacheItemOptions) (string, error) {
	response := make(chan CacheResponse)
	req := CacheReuest{
		key:       key,
		generator: generator,
		response:  response,
		ctx:       context.Background(),
	}

	for _, opt := range opts {
		opt(&req)
	}

	c.requests <- &req

	select {
	case <-req.ctx.Done():
		c.cancel <- &req
		return "", req.ctx.Err()
	case resp := <-response:
		return resp.value, resp.err
	}
}

// CacheStruct caches the result of a function that returns a struct.
// The key is used to identify the cached value.
// The generator function is called to generate the value if it is not already cached.
// The result parameter is a pointer to the struct that will be populated with the cached value.
// The opts parameter is optional and can be used to set additional cache item options.
// WithTTL sets TTL for cache item
// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
// Returns an error if there was a problem caching or unmarshalling the value.
func (c *Cache) CacheStruct(key string, generator func(context.Context) (any, error), result any, opts ...CacheItemOptions) error {
	generatorWrapper := func(ctx context.Context) (string, error) {
		val, err := generator(ctx)

		if err != nil {
			return "", err
		}

		jsonVal, err := json.Marshal(val)

		if err != nil {
			return "", err
		}

		return string(jsonVal), nil
	}

	val, err := c.Cache(key, generatorWrapper, opts...)

	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(val), result)

	return err
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

			ctx, cancel := context.WithCancel(context.Background())
			requestStorage[req.key] = CacheQueue{
				requests:  []*CacheReuest{req},
				cancelCtx: cancel,
			}

			reqCopy := *req
			reqCopy.ctx = ctx
			go c.processRequest(&reqCopy)

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
					close(req.response)
				}

				continue

			}

			for _, req := range reqQ.requests {
				req.response <- resp
			}

			delete(requestStorage, resp.key)

		case req := <-c.cancel:
			reqQ, ok := requestStorage[req.key]

			if !ok {
				continue
			}

			close(req.response)

			if len(reqQ.requests) == 1 {
				reqQ.cancelCtx()
				delete(requestStorage, req.key)
				continue
			}

			for i, r := range reqQ.requests {
				if r == req {
					reqQ.requests = append(reqQ.requests[:i], reqQ.requests[i+1:]...)
					requestStorage[req.key] = reqQ

					break
				}
			}
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
func (c *Cache) processRequest(req *CacheReuest) {
	var value string
	var err error
	var needWarmUp bool
	if req.WarmUpTTL.Nanoseconds() > 0 {
		var ttl time.Duration
		value, ttl, err = c.Storage.GetWithTTL(req.ctx, req.key)

		readyForWarmUp := ttl.Nanoseconds() != 0 && ttl.Nanoseconds() <= req.WarmUpTTL.Nanoseconds()
		if err == nil && readyForWarmUp {
			needWarmUp = true
		}
	} else {
		value, err = c.Storage.Get(req.ctx, req.key)
	}

	if err != nil && errors.Is(err, storage.KeyNotExistError{}) {
		value, err = c.generateAndSet(req)
	}

	cacheResp := CacheResponse{
		key:       req.key,
		value:     value,
		err:       err,
		warmingUp: needWarmUp,
	}

	c.responses <- cacheResp

	if needWarmUp {
		newVal, err := c.generateAndSet(req)

		cacheResp := CacheResponse{
			key:       req.key,
			value:     newVal,
			err:       err,
			warmingUp: false,
		}

		c.responses <- cacheResp
	}
}

// generateAndSet generates a value using the provided generator function,
// sets it in the cache storage with the given key and options,
// and returns the generated value and any error encountered.
func (c *Cache) generateAndSet(req *CacheReuest) (string, error) {
	value, err := req.generator(req.ctx)

	if err != nil {
		return value, err
	}

	ttl := randomizeTTL(c.maxShiftTTL, req.TTL)

	err = c.Storage.Set(req.ctx, req.key, value, storage.CacheStorageItemOptions{TTL: ttl})

	if err != nil {
		return value, err
	}

	return value, nil
}

// randomizeTTL randomizes the TTL (time-to-live) duration by a percentage defined by persentOfRandomTTL constant.
// It takes a time.Duration as input and returns a time.Duration as output.
// If the input duration is zero, it returns the same duration.
func randomizeTTL(maxShiftTTL uint8, ttl time.Duration) time.Duration {
	if maxShiftTTL == 0 || ttl.Nanoseconds() == 0 {
		return ttl
	}

	MaxShift := int64(ttl.Nanoseconds()) * int64(maxShiftTTL) / 100
	randomizedShift := int64(float64(MaxShift) * (float64(rand.Intn(100)) - 50.0) / 100.0)

	randomizedTTL := ttl.Nanoseconds() + randomizedShift
	return time.Duration(randomizedTTL)
}
