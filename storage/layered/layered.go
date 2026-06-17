package layered

// Layered cache storage allows to use multiple cache storages in a layered manner,
// This can be useful in scenarios where you want to have a fast in-memory cache as the first layer
// and a slower but more persistent cache (like Redis or Memcached) as the second layer.
// Even though layerd cache support any number of provided stores, it recommended to use not more that to layers,
// 1st layer for in-memory cache and 2nd layer for distributed cache, because of performance reasons,

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ksysoev/anycache"
)

const (
	minNumberOfStorages = 2
)

type Storage struct {
	stores []anycache.CacheStorage
}

// New creates a new layered cache storage with the provided cache storages.
// Layered cache storage allows to use multiple cache storages in a layered manner,
// where each layer can be a different type of cache storage (e.g., in-memory, Redis, Memcached).
func New(stores ...anycache.CacheStorage) (*Storage, error) {
	if len(stores) < minNumberOfStorages {
		return nil, fmt.Errorf("at least 2 storages are required, got %d", len(stores))
	}

	return &Storage{stores: stores}, nil
}

// Get retrieves the value associated with the provided key from the layered cache storage.
// if the key is found in a lower layer, it will be set in all upper layers to optimize future access.
func (s *Storage) Get(ctx context.Context, key string) ([]byte, error) {
	// unfortunately to populate upper layers of cache we need to get TTL of the key, so we need to use GetWithTTL method
	val, _, err := s.GetWithTTL(ctx, key)

	return val, err
}

// Set stores a value associated with the provided key in all layers of the cache storage.
func (s *Storage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	for i, store := range s.stores {
		err := store.Set(ctx, key, value, ttl)
		if err != nil {
			return fmt.Errorf("error setting key in store %d: %w", i, err)
		}
	}

	return nil
}

// GetWithTTL retrieves the value associated with the provided key from the layered cache storage along with its TTL.
// if the key is found in a lower layer, it will be set in all upper layers to optimize future access.
// for example, if the key is found in the third layer, it will be set in the first and second layers as well.
func (s *Storage) GetWithTTL(ctx context.Context, key string) ([]byte, time.Duration, error) {
	for i, store := range s.stores {
		val, ttl, err := store.GetWithTTL(ctx, key)
		if errors.Is(err, anycache.ErrKeyNotExists) {
			continue
		}

		if err != nil {
			return nil, 0, fmt.Errorf("error getting key from store %d: %w", i, err)
		}

		for j := i - 1; j >= 0; j-- {
			if err := s.stores[j].Set(ctx, key, val, ttl); err != nil {
				return nil, 0, fmt.Errorf("error back populating upper cache layers key in store %d: %w", j, err)
			}
		}

		return val, ttl, nil
	}

	return nil, 0, anycache.ErrKeyNotExists
}
