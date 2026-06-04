// Package anycache provide laze caching with posibility to use diffent cache storages
package anycache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/ksysoev/anycache/storage"
	"golang.org/x/sync/singleflight"
)

const (
	HundredPercent = 100
)

// CacheStorage
type CacheStorage interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, storage.CacheStorageItemOptions) error
	TTL(context.Context, string) (bool, time.Duration, error)
	Del(context.Context, string) (bool, error)
	GetWithTTL(context.Context, string) (string, time.Duration, error)
	Close() error
}

// Cache
type Cache struct {
	Storage     CacheStorage
	ctx         context.Context
	sf          singleflight.Group
	warmUpSF    singleflight.Group
	cancelCtx   context.CancelFunc
	cancel      chan *CacheReuest
	wg          sync.WaitGroup
	maxShiftTTL uint8
}

type CacheReuest struct {
	TTL       time.Duration
	WarmUpTTL time.Duration
}

type (
	CacheGenerator func(ctx context.Context) (string, error)
	CacheOptions   func(*Cache)
)

type CacheItemOptions func(*CacheReuest)

// NewCache creates a new Cache instance with the provided CacheStorage and CacheOptions.
// WithTTLRandomization sets max shift of TTL in persent
// It returns the created Cache instance.
func NewCache(store CacheStorage, opts ...CacheOptions) *Cache {
	ctx, cancelCtx := context.WithCancel(context.Background())

	c := Cache{
		Storage:   store,
		ctx:       ctx,
		cancelCtx: cancelCtx,
		cancel:    make(chan *CacheReuest),
	}

	for _, opt := range opts {
		opt(&c)
	}

	return &c
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

// Cache caches the result of the generator function for the given key.
// If the key already exists in the cache, the cached value is returned.
// Otherwise, the generator function is called to generate a new value,
// which is then cached and returned.
// The function takes an optional list of CacheItemOptions to customize the caching behavior.
// WithTTL sets TTL for cache item
// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func (c *Cache) Cache(ctx context.Context, key string, generator CacheGenerator, opts ...CacheItemOptions) (string, error) {
	var req CacheReuest

	for _, opt := range opts {
		opt(&req)
	}

	res := c.sf.DoChan(key, func() (any, error) {
		var (
			value      string
			err        error
			needWarmUp bool
		)

		if req.WarmUpTTL > 0 {
			var ttl time.Duration

			value, ttl, err = c.Storage.GetWithTTL(c.ctx, key)

			readyForWarmUp := ttl.Nanoseconds() != 0 && ttl.Nanoseconds() <= req.WarmUpTTL.Nanoseconds()
			if err == nil && readyForWarmUp {
				needWarmUp = true
			}
		} else {
			value, err = c.Storage.Get(c.ctx, key)
		}

		if err != nil && errors.Is(err, storage.KeyNotExistError{}) {
			value, err = c.generateAndSet(ctx, key, req.TTL, generator)
		}

		if needWarmUp {
			c.wg.Go(func() {
				_, _, _ = c.warmUpSF.Do(key, func() (any, error) {
					_, err := c.generateAndSet(ctx, key, req.TTL, generator)
					if err != nil {
						slog.Warn("Failed to warm up cache for key", "key", key, "error", err)
					}

					return nil, nil
				})
			})
		}

		return value, err
	})

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case resp := <-res:
		if resp.Err != nil {
			return "", resp.Err
		}

		val, ok := resp.Val.(string)
		if !ok {
			return "", errors.New("unexpected value type returned from generator")
		}

		return val, nil
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
func (c *Cache) CacheStruct(ctx context.Context, key string, generator func(context.Context) (any, error), result any, opts ...CacheItemOptions) error {
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

	val, err := c.Cache(ctx, key, generatorWrapper, opts...)
	if err != nil {
		return err
	}

	err = json.Unmarshal([]byte(val), result)

	return err
}

// generateAndSet generates a value using the provided generator function,
// sets it in the cache storage with the given key and options,
// and returns the generated value and any error encountered.
func (c *Cache) generateAndSet(ctx context.Context, key string, ttl time.Duration, generator CacheGenerator) (string, error) {
	value, err := generator(ctx)
	if err != nil {
		return value, err
	}

	ttl = randomizeTTL(c.maxShiftTTL, ttl)

	err = c.Storage.Set(ctx, key, value, storage.CacheStorageItemOptions{TTL: ttl})
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

	MaxShift := ttl.Nanoseconds() * int64(maxShiftTTL) / HundredPercent
	//nolint:gosec // we don't need cryptographically secure random number generator here
	randomizedShift := int64(float64(MaxShift) * (float64(rand.Intn(HundredPercent)) - HundredPercent/2) / HundredPercent)
	randomizedTTL := ttl.Nanoseconds() + randomizedShift

	return time.Duration(randomizedTTL)
}

// Close closes the Cache instance.
// It returns an error if any occurred.
func (c *Cache) Close() error {
	// cancel background requests
	c.cancelCtx()
	return c.Storage.Close()
}
