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
	MetricHook  func(key string, op State, latency time.Duration)
	shouldCache func(any) bool
	TTL         time.Duration
	WarmUpTTL   time.Duration
	Timeout     time.Duration
	// ignoreSetError makes a storage write failure non-fatal: the generated
	// value is still returned to all callers and the error is only logged.
	ignoreSetError bool
}

type (
	generator       func(ctx context.Context) ([]byte, bool, error)
	CacheGenerator  func(ctx context.Context) ([]byte, error)
	CacheGeneratorS func(ctx context.Context) (string, error)
	// CacheGeneratorC produces a value and reports whether it may be stored.
	// When cacheable is false the value is returned to the caller (and to all
	// concurrent de-duplicated callers) but is not written to the storage.
	CacheGeneratorC func(ctx context.Context) (data []byte, cacheable bool, err error)
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
	req := &Request{
		TTL:        ttl,
		MetricHook: c.observer,
	}

	for _, opt := range opts {
		opt(req)
	}

	gen := func(ctx context.Context) ([]byte, bool, error) {
		res, err := generator(ctx)
		if err != nil {
			return nil, false, err
		}

		if req.shouldCache != nil && !req.shouldCache(res) {
			return res, false, err
		}

		return res, true, nil
	}

	return c.cache(ctx, key, gen, req)
}

func (c *Cache) cache(ctx context.Context, key string, gen generator, req *Request) ([]byte, error) {
	if req.TTL <= 0 {
		return nil, errors.New("ttl must be greater than zero")
	}

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

	var (
		val *result
		err error
	)

	if req.shouldCache != nil {
		val, err = c.processRequest(ctx, key, gen, req)
	} else {
		val, err = c.processRequestWithDeDuplication(ctx, key, gen, req)
	}

	if err != nil {
		return nil, err
	}

	state = val.state

	return val.data, nil
}

// CacheC returns the cached value for key or generates it on miss, letting the
// generator decide per result whether the value is stored.
//
// Unlike WithShouldCache, CacheC keeps concurrent request de-duplication:
// concurrent callers for the same key share a single generator execution and
// all receive its result, whether or not it was stored.
//
// A failure to store a generated value is not returned as an error: it is
// logged and the generated value is still returned to all callers, so a
// failing storage does not make every caller regenerate the value.
// The WithShouldCache option is ignored, the generator decides cacheability.
// ttl must be greater than zero.
func (c *Cache) CacheC(ctx context.Context, key string, ttl time.Duration, gen CacheGeneratorC, opts ...CacheItemOptions) ([]byte, error) {
	req := &Request{
		TTL:        ttl,
		MetricHook: c.observer,
	}

	for _, opt := range opts {
		opt(req)
	}

	// shouldCache is not applicable here, the generator controls cacheability.
	req.shouldCache = nil
	req.ignoreSetError = true

	return c.cache(ctx, key, generator(gen), req)
}

// CacheS is a convenience method that wraps the Cache method to return a string value instead of a byte slice.
func (c *Cache) CacheS(ctx context.Context, key string, ttl time.Duration, generator CacheGeneratorS, opts ...CacheItemOptions) (string, error) {
	req := &Request{
		TTL:        ttl,
		MetricHook: c.observer,
	}

	for _, opt := range opts {
		opt(req)
	}

	generatorWrapper := func(ctx context.Context) ([]byte, bool, error) {
		res, err := generator(ctx)
		if err != nil {
			return nil, false, err
		}

		if req.shouldCache != nil && !req.shouldCache(res) {
			return []byte(res), false, err
		}

		return []byte(res), true, nil
	}

	val, err := c.cache(ctx, key, generatorWrapper, req)
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
	req := &Request{
		TTL:        ttl,
		MetricHook: c.observer,
	}

	for _, opt := range opts {
		opt(req)
	}

	generatorWrapper := func(ctx context.Context) ([]byte, bool, error) {
		val, err := generator(ctx)
		if err != nil {
			return nil, false, err
		}

		data, err := c.codec.Encode(val)
		if err != nil {
			return nil, false, err
		}

		if req.shouldCache != nil && !req.shouldCache(val) {
			return data, false, err
		}

		return data, true, nil
	}

	val, err := c.cache(ctx, key, generatorWrapper, req)
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
//
// The generator runs on ctx. The value is stored within what is left of the
// ctx deadline, but with at least storeGrace (capped at req.Timeout), so a
// generator that used most of its budget still leaves the write time to succeed.
// When req.ignoreSetError is set, a storage failure is logged and the value is returned without error.
func (c *Cache) generateAndSet(ctx, base context.Context, key string, req *Request, gen generator) ([]byte, error) {
	value, cacheable, err := gen(ctx)
	if err != nil {
		return value, err
	}

	if !cacheable {
		return value, nil
	}

	ttl := randomizeTTL(c.maxShiftTTL, req.TTL)

	storeCtx, cancel := storeContext(ctx, base, req.Timeout)
	defer cancel()

	err = c.Storage.Set(storeCtx, key, value, ttl)

	switch {
	case err == nil:
		return value, nil
	case req.ignoreSetError:
		slog.Warn("Failed to store generated value", "key", key, "error", err)
		return value, nil
	default:
		return value, err
	}
}

// withOptionalTimeout returns a context derived from ctx that times out after
// timeout, or ctx itself when timeout is not positive.
func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, timeout)
}

// storeGrace is the minimum time a storage write gets after generation, so a
// generator finishing close to its deadline does not leave the write without time.
const storeGrace = time.Second

// storeContext returns the context for storing a value generated on genCtx.
// It keeps the genCtx deadline but extends it to at least min(timeout, storeGrace)
// from now. The context derives from base, so it is still cancelled with base
// and never outlives a deadline set on base. Without a timeout it is base itself.
func storeContext(genCtx, base context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return base, func() {}
	}

	deadline := time.Now().Add(min(timeout, storeGrace))

	if genDeadline, ok := genCtx.Deadline(); ok && genDeadline.After(deadline) {
		deadline = genDeadline
	}

	return context.WithDeadline(base, deadline)
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

// processRequestWithDeDuplication processes a cache request for the given key using the provided generator function and request options.
func (c *Cache) processRequestWithDeDuplication(ctx context.Context, key string, generator generator, req *Request) (*result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Note: singleflight work should always run with a decoupled context
	// so that one caller cancellation doesn't affect other requests for the same key.
	// Use c.ctx (the cache base context) instead of the caller ctx here.
	res := c.sf.DoChan(key, func() (value any, err error) {
		reqLocal := req
		if reqLocal.Timeout <= 0 {
			if deadline, ok := ctx.Deadline(); ok {
				reqLocal.Timeout = time.Until(deadline)
				if reqLocal.Timeout <= 0 {
					return nil, context.DeadlineExceeded
				}
			}
		}

		return c.processRequest(c.ctx, key, generator, reqLocal)
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
func (c *Cache) processRequest(ctx context.Context, key string, generator generator, req *Request) (value *result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cache.Cache panicked: %v", r)
			value = nil
		}
	}()

	base := ctx

	ctx, cancel := withOptionalTimeout(base, req.Timeout)
	defer cancel()

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

		data, ttl, err = c.Storage.GetWithTTL(ctx, key)

		readyForWarmUp := ttl > 0 && ttl <= req.WarmUpTTL
		if err == nil && readyForWarmUp {
			needWarmUp = true
		}
	} else {
		data, err = c.Storage.Get(ctx, key)
	}

	switch {
	case errors.Is(err, ErrKeyNotExists):
		sfState = CacheMiss

		data, err = c.generateAndSet(ctx, base, key, req, generator)
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
func (c *Cache) startWarmUp(key string, generator generator, req *Request) {
	c.wg.Go(func() {
		defer c.warmUpLocks.Delete(key)

		ctx, cancel := withOptionalTimeout(c.ctx, req.Timeout)
		defer cancel()

		_, err := c.generateAndSet(ctx, c.ctx, key, req, generator)
		if err != nil {
			slog.Warn("Failed to warm up cache for key", "key", key, "error", err)
		}
	})
}
