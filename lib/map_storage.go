package anycache

type MapCacheStorage map[string]string

func (s MapCacheStorage) Get(key string) (string, error) {
	value, ok := s[key]

	if ok {
		return value, nil
	}

	return EMPTY_VALUE, KeyNotExistError{key: key}
}

func (s MapCacheStorage) Set(key string, value string) error {
	s[key] = value

	return nil
}

func (s MapCacheStorage) TTL(key string) (int64, error) {
	_, ok := s[key]

	if ok {
		return NO_EXPIRATION_KEY_TTL, nil
	}

	return NOT_EXISTEN_KEY_TTL, nil
}

func (s MapCacheStorage) Del(key string) (bool, error) {
	_, ok := s[key]

	if ok {
		delete(s, key)
		return true, nil
	}

	return false, nil
}
