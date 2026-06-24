// Package anycache provide laze caching with posibility to use diffent cache
// storages
package anycache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	HundredPercent = 100
)

var ErrKeyNotExists = errors.New("key does not exist")

// CacheStorage
type CacheStorage interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Del(context.Context, string) error
	GetWithTTL(context.Context, string) ([]byte, time.Duration, error)
}

// Cache
type Cache struct {
	Storage     CacheStorage
	ctx         context.Context
	sf          singleflight.Group
	cancelCtx   context.CancelFunc
	warmUpLocks sync.Map
	keyPrefix   string
	wg          sync.WaitGroup
	maxShiftTTL uint8
}

type CacheReuest struct {
	TTL       time.Duration
	WarmUpTTL time.Duration
}

type (
	CacheGenerator  func(ctx context.Context) ([]byte, error)
	CacheGeneratorS func(ctx context.Context) (string, error)
	CacheOptions    func(*Cache)
)

type CacheItemOptions func(*CacheReuest)

// New creates a new Cache instance with the provided CacheStorage and CacheOptions.
// WithTTLRandomization sets max shift of TTL in persent
// It returns the created Cache instance.
func New(store CacheStorage, opts ...CacheOptions) *Cache {
	ctx, cancelCtx := context.WithCancel(context.Background())

	c := Cache{
		Storage:   store,
		ctx:       ctx,
		cancelCtx: cancelCtx,
	}

	for _, opt := range opts {
		opt(&c)
	}

	return &c
}

// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func WithWarmUpTTL(ttl time.Duration) CacheItemOptions {
	return func(req *CacheReuest) {
		req.WarmUpTTL = ttl
	}
}

// Cache caches the result of the generator function for the given key, for the specified TTL (time-to-live) duration.
// If the key already exists in the cache, the cached value is returned.
// Otherwise, the generator function is called to generate a new value,
// which is then cached and returned.
// The function takes an optional list of CacheItemOptions to customize the caching behavior.
// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
func (c *Cache) Cache(ctx context.Context, key string, ttl time.Duration, generator CacheGenerator, opts ...CacheItemOptions) ([]byte, error) {
	if ttl <= 0 {
		return nil, errors.New("ttl must be greater than zero")
	}

	req := CacheReuest{
		TTL: ttl,
	}

	for _, opt := range opts {
		opt(&req)
	}

	key = c.keyPrefix + key

	res := c.sf.DoChan(key, func() (value any, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("cache.Cache panicked: %v", r)
				value = ""
			}
		}()

		var (
			needWarmUp         bool
			acquiredWarmUpLock bool
			warmUpStarted      bool
		)

		defer func() {
			if acquiredWarmUpLock && !warmUpStarted {
				c.warmUpLocks.Delete(key)
			}
		}()

		if req.WarmUpTTL > 0 {
			var ttl time.Duration

			_, warmUpLockBusy := c.warmUpLocks.LoadOrStore(key, struct{}{})
			acquiredWarmUpLock = !warmUpLockBusy

			value, ttl, err = c.Storage.GetWithTTL(c.ctx, key)

			readyForWarmUp := ttl > 0 && ttl <= req.WarmUpTTL
			if err == nil && readyForWarmUp {
				needWarmUp = true
			}
		} else {
			value, err = c.Storage.Get(c.ctx, key)
		}

		switch {
		case errors.Is(err, ErrKeyNotExists):
			value, err = c.generateAndSet(ctx, key, req.TTL, generator)
		case err != nil:
			return "", err
		}

		if needWarmUp && acquiredWarmUpLock {
			c.wg.Go(func() {
				defer c.warmUpLocks.Delete(key)

				_, err := c.generateAndSet(c.ctx, key, req.TTL, generator)
				if err != nil {
					slog.Warn("Failed to warm up cache for key", "key", key, "error", err)
				}
			})

			warmUpStarted = true
		}

		return value, err
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-res:
		if resp.Err != nil {
			return nil, resp.Err
		}

		val, ok := resp.Val.([]byte)
		if !ok {
			return nil, errors.New("unexpected value type returned from generator")
		}

		return val, nil
	}
}

// CacheS is a convenience method that wraps the Cache method to return a string value instead of a byte slice.
func (c *Cache) CacheS(ctx context.Context, key string, ttl time.Duration, generator CacheGeneratorS, opts ...CacheItemOptions) (string, error) {
	generatorWrapper := func(ctx context.Context) ([]byte, error) {
		result, err := generator(ctx)
		if err != nil {
			return nil, err
		}

		return []byte(result), nil
	}

	val, err := c.Cache(ctx, key, ttl, generatorWrapper, opts...)
	if err != nil {
		return "", err
	}

	return string(val), nil
}

// CacheStruct caches the result of a function that returns a struct.
// The key is used to identify the cached value.
// The generator function is called to generate the value if it is not already cached.
// The result parameter is a pointer to the struct that will be populated with the cached value.
// The opts parameter is optional and can be used to set additional cache item options.
// The ttl parameter must be greater than zero and controls the cache expiration.
// WithWarmUpTTL sets TTL threshold for cache item to be warmed up
// Returns an error if there was a problem caching or unmarshalling the value.
func (c *Cache) CacheStruct(ctx context.Context, key string, ttl time.Duration, generator func(context.Context) (any, error), result any, opts ...CacheItemOptions) error {
	generatorWrapper := func(ctx context.Context) ([]byte, error) {
		val, err := generator(ctx)
		if err != nil {
			return nil, err
		}

		jsonVal, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}

		return jsonVal, nil
	}

	val, err := c.Cache(ctx, key, ttl, generatorWrapper, opts...)
	if err != nil {
		return err
	}

	err = json.Unmarshal(val, result)

	return err
}

// Invalidate removes the cached value for the given key from the cache storage.
// accepts a context and the key to be invalidated.
// returns an error if there was a problem invalidating the cache for the key.
func (c *Cache) Invalidate(ctx context.Context, key string) error {
	key = c.keyPrefix + key

	err := c.Storage.Del(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to invalidate cache for key %s: %w", key, err)
	}

	return nil
}

// generateAndSet generates a value using the provided generator function,
// sets it in the cache storage with the given key and options,
// and returns the generated value and any error encountered.
func (c *Cache) generateAndSet(ctx context.Context, key string, ttl time.Duration, generator CacheGenerator) ([]byte, error) {
	value, err := generator(ctx)
	if err != nil {
		return value, err
	}

	ttl = randomizeTTL(c.maxShiftTTL, ttl)

	err = c.Storage.Set(ctx, key, value, ttl)
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
	c.wg.Wait()

	return nil
}
