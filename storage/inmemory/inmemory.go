package inmemory

import (
	"context"
	"time"
)

// type CacheStorage interface {
// 	Get(context.Context, string) (string, error)
// 	Set(context.Context, string, string, time.Duration) error
// 	TTL(context.Context, string) (bool, time.Duration, error)
// 	Del(context.Context, string) (bool, error)
// 	GetWithTTL(context.Context, string) (string, time.Duration, error)
// }

type InMemoryCacheStorage struct{}

func New() *InMemoryCacheStorage {
	return &InMemoryCacheStorage{}
}

func (s *InMemoryCacheStorage) Get(_ context.Context, key string) (string, error) {
	return "", nil
}

func (s *InMemoryCacheStorage) Set(_ context.Context, key string, value string, ttl time.Duration) error {
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
