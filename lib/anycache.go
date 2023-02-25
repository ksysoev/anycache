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

func (e KeyNotExistError) Error() string {
	return fmt.Sprintf("Key %s is not found", e.key)
}

func NewCache() Cache {
	return Cache{storage: MapCacheStorage{}}
}

func (c Cache) Cache(key string, newValue string) (string, error) {
	value, err := c.storage.Get(key)

	if err == nil {
		return value, nil
	}

	if !errors.Is(err, &KeyNotExistError{}) {
		return EMPTY_VALUE, err
	}

	err = c.storage.Set(key, newValue)

	if err != nil {
		return EMPTY_VALUE, err
	}

	return newValue, nil
}
