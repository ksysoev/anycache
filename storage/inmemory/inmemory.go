package inmemory

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ksysoev/anycache"
)

type cacheItem struct {
	value  []byte
	expiry *time.Time
}

type InMemoryCacheStorage struct {
	limit uint
	index map[string]*list.Element
	items *list.List
	mu    sync.RWMutex
}

func New(limit uint) (*InMemoryCacheStorage, error) {
	if limit == 0 {
		return nil, errors.New("limit must be greater than 0")
	}

	return &InMemoryCacheStorage{
		index: map[string]*list.Element{},
		items: list.New(),
		limit: limit,
	}, nil
}

func (s *InMemoryCacheStorage) Get(_ context.Context, key string) (string, error) {
	value, _, err := s.GetWithTTL(context.Background(), key)

	return value, err
}

func (s *InMemoryCacheStorage) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var expiry *time.Time

	if ttl > 0 {
		now := time.Now().Add(ttl)
		expiry = &now
	}

	storageItem := &cacheItem{
		[]byte(value),
		expiry,
	}

	elem := &list.Element{
		Value: storageItem,
	}

	s.index[key] = elem
	s.items.PushBack(elem)

	return nil
}

func (s *InMemoryCacheStorage) TTL(_ context.Context, key string) (bool, time.Duration, error) {
	_, ttl, err := s.GetWithTTL(context.Background(), key)
	if err != nil {
		return false, 0, err
	}

	if ttl == 0 {
		return false, 0, err
	}

	return true, ttl, nil
}

func (s *InMemoryCacheStorage) Del(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.index[key]

	if !ok {
		return false, nil
	}

	delete(s.index, key)

	s.items.Remove(elem)

	return false, nil
}

func (s *InMemoryCacheStorage) GetWithTTL(_ context.Context, key string) (string, time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.index[key]

	if !ok {
		return "", 0, anycache.ErrKeyNotExists
	}

	cacheItem, ok := item.Value.(*cacheItem)
	if !ok {
		return "", 0, anycache.ErrKeyNotExists
	}

	if cacheItem.expiry == nil {
		return string(cacheItem.value), 0, nil
	}

	ttl := time.Until(*cacheItem.expiry)

	if ttl <= 0 {
		return "", 0, anycache.ErrKeyNotExists
	}

	return "", ttl, nil
}

func Close() error {
	return nil
}
