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
	ctx     context.Context
	index   map[string]*cacheItem
	items   *list.List
	cancel  context.CancelFunc
	expiryQ expiryQueue
	wg      sync.WaitGroup
	limit   int
	mu      sync.RWMutex
}

func New(limit int) (*Storage, error) {
	if limit == 0 {
		return nil, errors.New("limit must be greater than 0")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())

	expiryQ := make(expiryQueue, 0, limit)
	heap.Init(&expiryQ)

	s := &Storage{
		index:   make(map[string]*cacheItem, limit),
		items:   list.New(),
		limit:   limit,
		expiryQ: expiryQ,
		ctx:     cancelCtx,
		cancel:  cancel,
	}

	s.wg.Go(s.expiryLoop)

	return s, nil
}

func (s *Storage) Get(_ context.Context, key string) (string, error) {
	value, _, err := s.GetWithTTL(context.Background(), key)

	return value, err
}

func (s *Storage) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if s.ctx.Err() != nil {
		return errors.New("storage is closed")
	}

	if ttl < 0 {
		return errors.New("ttl must be non-negative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.index[key]; ok {
		s.delete(existing)
	}

	if len(s.index) >= s.limit {
		leastUsed := s.items.Front()
		item, _ := leastUsed.Value.(*cacheItem)
		s.delete(item)
	}

	var expiry *time.Time

	if ttl > 0 {
		expTime := time.Now().Add(ttl)
		expiry = &expTime
	}

	item := &cacheItem{
		value:  []byte(value),
		expiry: expiry,
	}

	elem := &list.Element{
		Value: item,
	}

	item.lruPos = elem

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
	if s.ctx.Err() != nil {
		return false, errors.New("storage is closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.index[key]
	if !ok {
		return false, nil
	}

	if item.expiry != nil && time.Until(*item.expiry) <= 0 {
		s.delete(item)
		return false, nil
	}

	s.delete(item)

	return true, nil
}

func (s *Storage) delete(item *cacheItem) {
	delete(s.index, item.key)
	s.items.Remove(item.lruPos)
	if item.expiryPos >= 0 {
		heap.Remove(&s.expiryQ, item.expiryPos)
	}
}

func (s *Storage) GetWithTTL(_ context.Context, key string) (string, time.Duration, error) {
	if s.ctx.Err() != nil {
		return "", 0, errors.New("storage is closed")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	item, ok := s.index[key]

	if !ok {
		return "", 0, anycache.ErrKeyNotExists
	}

	if item.expiry == nil {
		s.items.MoveToBack(item.lruPos)
		return string(item.value), 0, nil
	}

	ttl := time.Until(*item.expiry)
	if ttl <= 0 {
		s.delete(item)

		return "", 0, anycache.ErrKeyNotExists
	}

	s.items.MoveToBack(item.lruPos)

	return string(item.value), ttl, nil
}

func (s *Storage) expiryLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.expiryQ.Len() == 0 {
				continue
			}

			for {
				item := s.expiryQ[0]
				if time.Until(*item.expiry) > 0 {
					break
				}

				s.delete(item)
			}
			s.mu.Unlock()
		}
	}
}

func (s *Storage) Close() error {
	if s.ctx.Err() != nil {
		return errors.New("storage is already closed")
	}

	s.cancel()
	s.wg.Wait()

	return nil
}
