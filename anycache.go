// Package anycache provides lazy cache-aside helpers with pluggable storage backends.
package anycache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/ksysoev/anycache/codec/json"
	"golang.org/x/sync/singleflight"
)

const (
	HundredPercent = 100
)

type State string

const (
	CacheHit    State = "hit"
	CacheMiss   State = "miss"
	CacheWarmUp State = "warm_up"
	CacheError  State = "error"
)

type result struct {
	state State
	data  []byte
}

var ErrKeyNotExists = errors.New("key does not exist")

// CacheStorage defines the backend contract used by Cache.
type CacheStorage interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Del(context.Context, string) error
	GetWithTTL(context.Context, string) ([]byte, time.Duration, error)
}

// Cache provides cache-aside operations on top of a CacheStorage backend.
type Cache struct {
	Storage     CacheStorage
	codec       Codec
	ctx         context.Context
	sf          singleflight.Group
	cancelCtx   context.CancelFunc
	observer    func(key string, op State, latency time.Duration)
	warmUpLocks sync.Map
	keyPrefix   string
	wg          sync.WaitGroup
	maxShiftTTL uint8
}

type Request struct {
	ctx         context.Context
	MetricHook  func(key string, op State, latency time.Duration)
	TTL         time.Duration
	WarmUpTTL   time.Duration
	Timeout     time.Duration
	isCacheable func([]byte) bool
}

type (
	generator       func(ctx context.Context) ([]byte, bool, error)
	CacheGenerator  func(ctx context.Context) ([]byte, error)
	CacheGeneratorS func(ctx context.Context) (string, error)
	CacheOptions    func(*Cache)
)

type CacheItemOptions func(*Request)

// New creates a Cache with the provided storage backend and options.
func New(store CacheStorage, opts ...CacheOptions) *Cache {
	ctx, cancelCtx := context.WithCancel(context.Background())

	c := Cache{
		Storage:   store,
		ctx:       ctx,
		cancelCtx: cancelCtx,
		codec:     json.Codec{},
	}

	for _, opt := range opts {
		opt(&c)
	}

	return &c
}

// Cache returns the cached value for key or generates and stores it on miss.
//
// ttl must be greater than zero.
// opts can customize request behavior (for example warm-up, timeout,
// or per-request metrics).
func (c *Cache) Cache(ctx context.Context, key string, ttl time.Duration, generator CacheGenerator, opts ...CacheItemOptions) ([]byte, error) {
	if ttl <= 0 {
		return nil, errors.New("ttl must be greater than zero")
	}

	req := Request{
		TTL:        ttl,
		MetricHook: c.observer,
	}

	for _, opt := range opts {
		opt(&req)
	}

	req.ctx = ctx

	state := CacheError
	start := time.Now()

	if req.MetricHook != nil {
		defer func(key string, start time.Time) {
			defer func() {
				if err := recover(); err != nil {
					slog.Error("MetricHook panicked", "key", key, "error", err)
				}
			}()

			req.MetricHook(key, state, time.Since(start))
		}(key, start)
	}

	// appending common key prefix after  Metrics hook, to simplify key parsing for observability
	key = c.keyPrefix + key
	gen := func(ctx context.Context) ([]byte, bool, error) {
		res, err := generator(ctx)
		if err != nil {
			return nil, false, err
		}

		if req.isCacheable != nil && !req.isCacheable(res) {
			return res, false, err
		}

		return res, true, nil
	}

	var (
		val *result
		err error
	)

	if req.isCacheable != nil {
		val, err = c.processRequest(key, gen, req)
	} else {
		val, err = c.processRequestWithDeDuplication(ctx, key, gen, req)
	}

	if err != nil {
		return nil, err
	}

	state = val.state

	return val.data, nil
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

// CacheStruct caches a generated value as encoded bytes string and decodes it into result.
//
// result must be a pointer that can be unmarshaled into.
// ttl must be greater than zero.
func (c *Cache) CacheStruct(ctx context.Context, key string, ttl time.Duration, generator func(context.Context) (any, error), result any, opts ...CacheItemOptions) error {
	generatorWrapper := func(ctx context.Context) ([]byte, error) {
		val, err := generator(ctx)
		if err != nil {
			return nil, err
		}

		data, err := c.codec.Encode(val)
		if err != nil {
			return nil, err
		}

		return data, nil
	}

	val, err := c.Cache(ctx, key, ttl, generatorWrapper, opts...)
	if err != nil {
		return err
	}

	err = c.codec.Decode(val, result)

	return err
}

// Invalidate removes key from the underlying cache storage.
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
func (c *Cache) generateAndSet(ctx context.Context, key string, ttl time.Duration, generator generator) ([]byte, error) {
	value, cachable, err := generator(ctx)
	if err != nil {
		return value, err
	}

	if !cachable {
		return value, nil
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

// Close cancels background work and waits for warm-up goroutines to finish.
func (c *Cache) Close() error {
	// cancel background requests
	c.cancelCtx()
	c.wg.Wait()

	return nil
}

func (c *Cache) processRequestWithDeDuplication(ctx context.Context, key string, generator generator, req Request) (*result, error) {
	res := c.sf.DoChan(key, func() (value any, err error) {
		return c.processRequest(key, generator, req)
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-res:
		if resp.Err != nil {
			return nil, resp.Err
		}

		val, ok := resp.Val.(*result)
		if !ok {
			return nil, errors.New("unexpected value type returned from cache processing")
		}

		return val, nil
	}
}

// processRequest processes a cache request for the given key using the provided generator function and request options.
func (c *Cache) processRequest(key string, generator generator, req Request) (value *result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cache.Cache panicked: %v", r)
			value = nil
		}
	}()

	if req.Timeout > 0 {
		var cancel context.CancelFunc

		req.ctx, cancel = context.WithTimeout(c.ctx, req.Timeout)
		defer cancel()
	}

	var (
		sfState            State
		data               []byte
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

		data, ttl, err = c.Storage.GetWithTTL(req.ctx, key)

		readyForWarmUp := ttl > 0 && ttl <= req.WarmUpTTL
		if err == nil && readyForWarmUp {
			needWarmUp = true
		}
	} else {
		data, err = c.Storage.Get(req.ctx, key)
	}

	switch {
	case errors.Is(err, ErrKeyNotExists):
		sfState = CacheMiss

		data, err = c.generateAndSet(req.ctx, key, req.TTL, generator)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		sfState = CacheHit
	}

	if needWarmUp && acquiredWarmUpLock {
		c.startWarmUp(key, generator, req)

		warmUpStarted = true
		sfState = CacheWarmUp
	}

	return &result{data: data, state: sfState}, err
}

// startWarmUp starts a background goroutine to warm up the cache for the given key
// using the provided generator function and request options.
func (c *Cache) startWarmUp(key string, generator generator, req Request) {
	c.wg.Go(func() {
		defer c.warmUpLocks.Delete(key)

		ctx := c.ctx

		if req.Timeout > 0 {
			var cancel context.CancelFunc

			ctx, cancel = context.WithTimeout(ctx, req.Timeout)
			defer cancel()
		}

		_, err := c.generateAndSet(ctx, key, req.TTL, generator)
		if err != nil {
			slog.Warn("Failed to warm up cache for key", "key", key, "error", err)
		}
	})
}
