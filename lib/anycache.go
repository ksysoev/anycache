package anycache

import (
	"errors"
	"fmt"
)

const NOT_EXISTEN_KEY_TTL = -2
const NO_EXPIRATION_KEY_TTL = -1

const EMPTY_VALUE = ""

type CacheStorage interface {
	Get(string) (string, error)
	Set(string, string) error
	TTL(string) (int64, error)
	Del(string) (bool, error)
}
type Cache struct {
	storage CacheStorage
}

type ValueGenerator func() (string, error)

func (e KeyNotExistError) Error() string {
	return fmt.Sprintf("Key %s is not found", e.key)
}

func NewCache() Cache {
	return Cache{storage: MapCacheStorage{}}
}

func (c Cache) Cache(key string, generator ValueGenerator) (string, error) {
	value, err := c.storage.Get(key)

	if err == nil {
		return value, nil
	}

	if !errors.Is(err, KeyNotExistError{key}) {
		return EMPTY_VALUE, err
	}

	newValue, err := generator()

	if err != nil {
		return EMPTY_VALUE, err
	}

	err = c.storage.Set(key, newValue)

	if err != nil {
		return EMPTY_VALUE, err
	}

	return newValue, nil
}
