package memcache

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/ksysoev/anycache"
	pb "github.com/ksysoev/anycache/storage/memcache/proto"
	"google.golang.org/protobuf/proto"
)

type Storage struct {
	memcache *memcache.Client
}

// New creates a new memcache cache storage with the provided memcache client.
func New(client *memcache.Client) *Storage {
	return &Storage{client}
}

// Get retrieves the value associated with the provided key from the Memcached cache storage.
// It returns the value as a byte array and an error if any occurred.
// If the key does not exist, it returns a nil and a ErrKeyNotExists.
// If any other error occurs during the operation, it returns a nil and the error.
func (s *Storage) Get(_ context.Context, key string) ([]byte, error) {
	raw, err := s.memcache.Get(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, anycache.ErrKeyNotExists
	}

	if err != nil {
		return nil, err
	}

	item, err := decode(raw.Value)
	if err != nil {
		return nil, err
	}

	return item.Value, nil
}

// Set stores value under key with an optional TTL.
// ttl <= 0 means the item never expires.
func (s *Storage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl.Seconds() > math.MaxInt32 {
		return errors.New("TTL value is too large")
	}

	var (
		expiresAt  time.Time
		expSeconds int32
	)

	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
		expSeconds = int32(ttl.Seconds())
	}

	data, err := encode(value, expiresAt)
	if err != nil {
		return err
	}

	return s.memcache.Set(&memcache.Item{
		Key:        key,
		Value:      data,
		Expiration: expSeconds,
	})
}

// TTL returns the remaining TTL for key.
//
//   - (false, 0, ErrKeyNotExists) – key not found or already past its expiry.
//   - (false, 0, nil)             – key exists but has no expiration.
//   - (true,  d, nil)             – key exists and expires in d.
func (s *Storage) TTL(_ context.Context, key string) (bool, time.Duration, error) {
	raw, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, 0, anycache.ErrKeyNotExists
	}

	if err != nil {
		return false, 0, err
	}

	item, err := decode(raw.Value)
	if err != nil {
		return false, 0, err
	}

	if item.ExpiresAtUnixMs == 0 {
		// Item was stored without a TTL.
		return false, 0, nil
	}

	expiresAt := time.UnixMilli(item.ExpiresAtUnixMs)
	remaining := time.Until(expiresAt)

	if remaining < 0 {
		// Protobuf-layer expiry elapsed but Memcached hasn't evicted yet.
		return true, 0, nil
	}

	return true, remaining, nil
}

// GetWithTTL retrieves both the value and its remaining TTL in one call.
func (s *Storage) GetWithTTL(_ context.Context, key string) ([]byte, time.Duration, error) {
	raw, err := s.memcache.Get(key)

	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, 0, anycache.ErrKeyNotExists
	}

	if err != nil {
		return nil, 0, err
	}

	item, err := decode(raw.Value)
	if err != nil {
		return nil, 0, err
	}

	if item.ExpiresAtUnixMs == 0 {
		return item.Value, 0, nil
	}

	expiresAt := time.UnixMilli(item.ExpiresAtUnixMs)
	remaining := time.Until(expiresAt)

	if remaining <= 0 {
		return nil, 0, anycache.ErrKeyNotExists
	}

	return item.Value, remaining, nil
}

// Del deletes key. Returns (false, ErrKeyNotExists) if the key did not exist.
func (s *Storage) Del(_ context.Context, key string) (bool, error) {
	err := s.memcache.Delete(key)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return false, anycache.ErrKeyNotExists
	}

	if err != nil {
		return false, err
	}

	return true, nil
}

// encode serialises value + optional expiry into a protobuf CachedItem.
// expiresAt zero means no TTL.
func encode(value []byte, expiresAt time.Time) ([]byte, error) {
	item := &pb.CachedItem{Value: value}
	if !expiresAt.IsZero() {
		item.ExpiresAtUnixMs = expiresAt.UnixMilli()
	}

	return proto.Marshal(item)
}

// decode deserialises a protobuf CachedItem from raw bytes.
func decode(raw []byte) (*pb.CachedItem, error) {
	item := &pb.CachedItem{}
	if err := proto.Unmarshal(raw, item); err != nil {
		return nil, err
	}

	return item, nil
}
