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

type KeyNotExistError struct {
	key string
}

func (e KeyNotExistError) Error() string {
	return fmt.Sprintf("Key %s is not found", e.key)
}

func NewCache() Cache {
	return Cache{storage: SimpleStorage{}}
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

type SimpleStorage map[string]string

func (s SimpleStorage) Get(key string) (string, error) {
	value, ok := s[key]

	if ok {
		return value, nil
	}

	return EMPTY_VALUE, KeyNotExistError{key: key}
}

func (s SimpleStorage) Set(key string, value string) error {
	s[key] = value

	return nil
}

func (s SimpleStorage) TTL(key string) (int64, error) {
	_, ok := s[key]

	if ok {
		return NO_EXPIRATION_KEY_TTL, nil
	}

	return NOT_EXISTEN_KEY_TTL, nil
}

func (s SimpleStorage) Del(key string) (bool, error) {
	_, ok := s[key]

	if ok {
		delete(s, key)
		return true, nil
	}

	return false, nil
}
