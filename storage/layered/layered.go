package layered

// Layered cache storage composes multiple backends as ordered layers.
//
// Typical setup: fast local L1 (in-memory) + shared slower L2 (Redis/Memcached).
//
// Semantics caveat:
//   - Operations are sequential across layers, not transactional.
//   - Set/Del can partially apply (earlier layers may succeed before later-layer error).
//   - Reads may succeed in a lower layer but still return error if back-population fails.
//
// Consistency model is best effort eventual alignment between layers.
// Temporary divergence is possible during failures.

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

// Storage accesses layers from index 0 (top/fastest) to the last (lowest tier).
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

	for i, store := range stores {
		if store == nil {
			return nil, fmt.Errorf("storage %d is nil", i)
		}
	}

	return &Storage{stores: stores}, nil
}

// Get retrieves the value associated with key from layered cache storage.
// If the key is found in a lower layer, upper layers are back-populated via GetWithTTL.
func (s *Storage) Get(ctx context.Context, key string) ([]byte, error) {
	// Back-population requires the key TTL, so Get delegates to GetWithTTL.
	val, _, err := s.GetWithTTL(ctx, key)

	return val, err
}

// Set writes key/value/ttl to layers in order (0..N-1).
//
// This operation is non-atomic across layers: on first error it returns immediately,
// and previously written layers are not rolled back.
func (s *Storage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	for i, store := range s.stores {
		err := store.Set(ctx, key, value, ttl)
		if err != nil {
			return fmt.Errorf("error setting key in store %d: %w", i, err)
		}
	}

	return nil
}

// GetWithTTL reads layers in order and returns the first hit.
//
// Read/error semantics:
//   - ErrKeyNotExists from a layer is treated as miss and lookup continues.
//   - Any other layer read error fails the call immediately.
//
// Back-population semantics:
//   - On lower-layer hit, upper layers [0..i-1] are populated via Set using returned ttl.
//   - If any back-population Set fails, GetWithTTL returns an error even though
//     a lower layer had a value.
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

// Del deletes key from layers in order (0..N-1).
//
// This operation is non-atomic across layers: on first error it returns immediately,
// and previously deleted layers are not restored.
func (s *Storage) Del(ctx context.Context, key string) error {
	for i, store := range s.stores {
		err := store.Del(ctx, key)
		if err != nil {
			return fmt.Errorf("error deleting key from store %d: %w", i, err)
		}
	}

	return nil
}
