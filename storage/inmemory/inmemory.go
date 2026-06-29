package inmemory

import (
	"container/heap"
	"container/list"
	"context"
	"errors"
	"slices"
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
	mu      sync.Mutex
}

// New creates a new in-memory cache storage with the specified limit on the number of items.
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

// Get retrieves the value associated with the provided key from the in-memory cache storage.
func (s *Storage) Get(_ context.Context, key string) ([]byte, error) {
	value, _, err := s.GetWithTTL(context.Background(), key)

	return value, err
}

// Set stores a value associated with the provided key in the in-memory cache storage.
func (s *Storage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := s.checkCtx(ctx); err != nil {
		return err
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

		item, ok := leastUsed.Value.(*cacheItem)
		if !ok {
			return errors.New("failed to retrieve least recently used item, invalid type assertion")
		}

		s.delete(item)
	}

	var expiry *time.Time = nil

	if ttl > 0 {
		expTime := time.Now().Add(ttl)
		expiry = &expTime
	}

	item := &cacheItem{
		key:       key,
		value:     value,
		expiry:    expiry,
		expiryPos: -1,
	}

	elem := s.items.PushBack(item)

	item.lruPos = elem
	s.index[key] = item

	if item.expiry != nil {
		s.expiryQ.Push(item)
	}

	return nil
}

// Del deletes the value associated with the provided key from the in-memory cache storage.
func (s *Storage) Del(ctx context.Context, key string) error {
	if err := s.checkCtx(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.index[key]
	if !ok {
		return nil
	}

	if item.expiry != nil && time.Until(*item.expiry) <= 0 {
		s.delete(item)
		return nil
	}

	s.delete(item)

	return nil
}

// GetWithTTL retrieves the value and time-to-live (TTL) associated with the provided key from the in-memory cache storage.
func (s *Storage) GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, error) {
	if err := s.checkCtx(ctx); err != nil {
		return nil, 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	item, ok := s.index[key]

	if !ok {
		return nil, 0, anycache.ErrKeyNotExists
	}

	if item.expiry == nil {
		s.items.MoveToBack(item.lruPos)
		return slices.Clone(item.value), 0, nil
	}

	ttl := time.Until(*item.expiry)
	if ttl <= 0 {
		s.delete(item)

		return nil, 0, anycache.ErrKeyNotExists
	}

	s.items.MoveToBack(item.lruPos)

	return slices.Clone(item.value), ttl, nil
}

// Close gracefully shuts down the in-memory cache storage, ensuring that all resources are released and any ongoing operations are completed.
func (s *Storage) Close() error {
	if s.ctx.Err() != nil {
		return errors.New("storage is already closed")
	}

	s.cancel()
	s.wg.Wait()

	return nil
}

// delete removes the item from the cache, including the index, LRU list, and expiry queue.
func (s *Storage) delete(item *cacheItem) {
	delete(s.index, item.key)
	s.items.Remove(item.lruPos)

	if item.expiryPos >= 0 {
		heap.Remove(&s.expiryQ, item.expiryPos)
	}
}

// expiryLoop continuously checks for expired items in the cache and removes them.
func (s *Storage) expiryLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()

			for s.expiryQ.Len() > 0 {
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

// checkCtx checks the context of the storage and the provided context for cancellation or closure.
func (s *Storage) checkCtx(ctx context.Context) error {
	if s.ctx.Err() != nil {
		return errors.New("storage is closed")
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	return nil
}
