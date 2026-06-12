package inmemory

import (
	"container/heap"
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ksysoev/anycache"
)

type cacheItem struct {
	key       string
	expiry    *time.Time
	value     []byte
	expiryPos int
	lruPos    list.Element
}

type Storage struct {
	index   map[string]*cacheItem
	items   *list.List
	expiryQ expiryQueue
	limit   uint
	mu      sync.RWMutex
}

func New(limit uint) (*Storage, error) {
	if limit == 0 {
		return nil, errors.New("limit must be greater than 0")
	}

	expiryQ := make(expiryQueue, 0, limit)
	heap.Init(&expiryQ)

	return &Storage{
		index:   make(map[string]*cacheItem, limit),
		items:   list.New(),
		limit:   limit,
		expiryQ: expiryQ,
	}, nil
}

func (s *Storage) Get(_ context.Context, key string) (string, error) {
	value, _, err := s.GetWithTTL(context.Background(), key)

	return value, err
}

func (s *Storage) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if ttl < 0 {
		return errors.New("ttl must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var expiry *time.Time

	if ttl > 0 {
		expTime := time.Now().Add(ttl)
		expiry = &expTime
	}

	elem := &list.Element{
		Value: key,
	}

	item := &cacheItem{
		value:  []byte(value),
		expiry: expiry,
	}

	s.index[key] = item
	s.items.PushBack(elem)
	s.expiryQ.Push(item)

	return nil
}

func (s *Storage) TTL(_ context.Context, key string) (bool, time.Duration, error) {
	_, ttl, err := s.GetWithTTL(context.Background(), key)
	if err != nil {
		return false, 0, err
	}

	if ttl == 0 {
		return false, 0, err
	}

	return true, ttl, nil
}

func (s *Storage) Del(_ context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	elem, ok := s.index[key]

	if !ok {
		return false, nil
	}

	_, err := exractItem(*elem)

	delete(s.index, key)

	s.items.Remove(elem)

	if errors.Is(err, anycache.ErrKeyNotExists) {
		return false, nil
	}

	return true, nil
}

func (s *Storage) GetWithTTL(_ context.Context, key string) (string, time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.index[key]

	if !ok {
		return "", 0, anycache.ErrKeyNotExists
	}

	cacheItem, err := exractItem(*item)

	switch {
	case errors.Is(err, anycache.ErrKeyNotExists):
		delete(s.index, key)
		s.items.Remove(item)

		return "", 0, err
	case err != nil:
		return "", 0, err
	}

	var ttl time.Duration
	if cacheItem.expiry != nil {
		ttl = time.Until(*cacheItem.expiry)
	}

	return string(cacheItem.value), ttl, nil
}

func exractItem(el list.Element) (*cacheItem, error) {
	cacheItem, ok := el.Value.(*cacheItem)
	if !ok {
		return nil, anycache.ErrKeyNotExists
	}

	if cacheItem.expiry == nil {
		return cacheItem, nil
	}

	ttl := time.Until(*cacheItem.expiry)

	if ttl <= 0 {
		return nil, anycache.ErrKeyNotExists
	}

	return cacheItem, nil
}

func Close() error {
	return nil
}
