package inmemory

import (
	"container/list"
	"context"
	"sync"
	"time"
)

type cacheItem struct {
	value  []byte
	expiry *time.Time
}

type InMemoryCacheStorage struct {
	index *sync.Map
	items *list.List
}

func New() *InMemoryCacheStorage {
	return &InMemoryCacheStorage{
		index: &sync.Map{},
		items: list.New(),
	}
}

func (s *InMemoryCacheStorage) Get(_ context.Context, key string) (string, error) {
	return "", nil
}

func (s *InMemoryCacheStorage) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	var expiry *time.Time

	if ttl > 0 {
		now := time.Now().Add(ttl)
		expiry = &now
	}

	storageItem := &cacheItem{
		[]byte(value),
		expiry,
	}

	s.index.Store(key, storageItem)
	s.items.PushBack(storageItem)

	return nil
}

func TTL(_ context.Context, key string) (bool, time.Duration, error) {
	return false, 0, nil
}

func Del(_ context.Context, key string) (bool, error) {
	return false, nil
}

func GetWithTTL(_ context.Context, key string) (string, time.Duration, error) {
	return "", 0, nil
}

func Close() error {
	return nil
}
