package badger

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/ksysoev/anycache"
)

type Storage struct {
	client *badger.DB
}

func New(client *badger.DB) *Storage {
	return &Storage{
		client: client,
	}
}

// Get retrieves the value associated with the provided key from the Badger cache storage.
func (s *Storage) Get(_ context.Context, key string) ([]byte, error) {
	var val []byte

	err := s.client.View(func(txn *badger.Txn) error {
		v, err := txn.Get([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return anycache.ErrKeyNotExists
		}

		if err != nil {
			return err
		}

		err = v.Value(func(valBytes []byte) error {
			val = append([]byte{}, valBytes...)

			return nil
		})
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return val, nil
}

// Set stores a value associated with the provided key in the Badger cache storage with an optional TTL.
func (s *Storage) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Update(func(txn *badger.Txn) error {
		entity := badger.NewEntry([]byte(key), value)
		if ttl > 0 {
			entity.WithTTL(ttl)
		}

		return txn.SetEntry(entity)
	})
}

// Del removes the value associated with the provided key from the Badger cache storage.
func (s *Storage) Del(_ context.Context, key string) error {
	return s.client.Update(func(txn *badger.Txn) error {
		err := txn.Delete([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}

		return err
	})
}

// GetWithTTL retrieves the value and time-to-live (TTL) associated with the provided key from the Badger cache storage.
func (s *Storage) GetWithTTL(_ context.Context, key string) ([]byte, time.Duration, error) {
	var (
		val []byte
		ttl time.Duration
	)

	err := s.client.View(func(txn *badger.Txn) error {
		v, err := txn.Get([]byte(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return anycache.ErrKeyNotExists
		}

		if err != nil {
			return err
		}

		err = v.Value(func(valBytes []byte) error {
			val = append([]byte{}, valBytes...)

			return nil
		})
		if err != nil {
			return err
		}

		expiresAt := v.ExpiresAt()
		if expiresAt == 0 {
			return nil
		} else if expiresAt > math.MaxInt64 {
			return errors.New("expiresAt value exceeds maximum int64 value")
		}

		expiresAtTime := time.Unix(int64(expiresAt), 0)

		remaining := time.Until(expiresAtTime)
		if remaining <= 0 {
			return nil
		}

		ttl = remaining

		return nil
	})

	return val, ttl, err
}
