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
	expiry    *time.Time
	lruPos    *list.Element
	key       string
	value     []byte
	expiryPos int
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
		lruPos: elem,
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

	item, ok := s.index[key]
	if !ok {
		return false, nil
	}

	if item.expiry != nil && time.Until(*item.expiry) <= 0 {
		return false, nil
	}

	s.delete(item)

	return true, nil
}

func (s *Storage) delete(item *cacheItem) {
	delete(s.index, item.key)
	s.items.Remove(item.lruPos)
	heap.Remove(&s.expiryQ, item.expiryPos)
}

func (s *Storage) GetWithTTL(_ context.Context, key string) (string, time.Duration, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.index[key]

	if !ok {
		return "", 0, anycache.ErrKeyNotExists
	}

	if item.expiry == nil {
		return string(item.value), 0, nil
	}

	ttl := time.Until(*item.expiry)
	if ttl <= 0 {
		s.delete(item)

		return "", 0, anycache.ErrKeyNotExists
	}

	return string(item.value), ttl, nil
}

func Close() error {
	return nil
}
