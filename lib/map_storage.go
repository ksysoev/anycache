package anycache

type MapCacheStorage[K comparable, V any] map[K]V

func (s MapCacheStorage[K, V]) Get(key K) (V, error) {
	value, ok := s[key]

	if ok {
		return value, nil
	}

	return value, KeyNotExistError{}
}

func (s MapCacheStorage[K, V]) Set(key K, value V) error {
	s[key] = value

	return nil
}

func (s MapCacheStorage[K, V]) TTL(key K) (int64, error) {
	_, ok := s[key]

	if ok {
		return NO_EXPIRATION_KEY_TTL, nil
	}

	return NOT_EXISTEN_KEY_TTL, nil
}

func (s MapCacheStorage[K, V]) Del(key K) (bool, error) {
	_, ok := s[key]

	if ok {
		delete(s, key)
		return true, nil
	}

	return false, nil
}
