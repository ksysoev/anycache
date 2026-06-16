package layered

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ksysoev/anycache"
)

type Storage struct {
	stores []anycache.CacheStorage
}

func New(stores ...anycache.CacheStorage) (*Storage, error) {
	if len(stores) < 2 {
		return nil, fmt.Errorf("at least 2 storages are required, got %d", len(stores))
	}

	return &Storage{stores: stores}, nil
}

func (s *Storage) Get(ctx context.Context, key string) ([]byte, error) {
	val, _, err := s.GetWithTTL(ctx, key)

	return val, err
}

func (s *Storage) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	for i, store := range s.stores {
		err := store.Set(ctx, key, value, ttl)
		if err != nil {
			return fmt.Errorf("error setting key in store %d: %w", i, err)
		}
	}

	return nil
}

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
				slog.Warn("Failed to set key in upper layer store", "store_index", j, "key", key, "error", err)
			}
		}

		return val, ttl, nil
	}

	return nil, 0, anycache.ErrKeyNotExists
}
