package anycache

import (
	"errors"
	"sync"
	"time"
)

const NOT_EXISTEN_KEY_TTL = -2
const NO_EXPIRATION_KEY_TTL = -1

const EMPTY_VALUE = ""

type CacheStorage[K comparable, V any] interface {
	Get(K) (V, error)
	Set(K, V, CacheItemOptions) error
	TTL(K) (int64, error)
	Del(K) (bool, error)
}
type Cache[K comparable, V any] struct {
	storage    CacheStorage[K, V]
	globalLock sync.Mutex
	locks      map[K]*sync.Mutex
}

type CacheItemOptions struct {
	ttl time.Duration
}

func NewCache[K comparable, V any]() Cache[K, V] {
	return Cache[K, V]{
		storage:    MapCacheStorage[K, V]{},
		globalLock: sync.Mutex{},
		locks:      map[K]*sync.Mutex{},
	}
}

func (c *Cache[K, V]) Cache(key K, generator func() (V, error), options CacheItemOptions) (V, error) {
	value, err := c.storage.Get(key)

	if err == nil {
		return value, nil
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
