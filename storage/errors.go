package storage

// KeyNotExistError is an error that is returned when key is not found in cache
type KeyNotExistError struct{}

func (KeyNotExistError) Error() string {
	return "Key is not found"
}
