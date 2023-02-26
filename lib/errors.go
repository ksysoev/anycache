package anycache

type KeyNotExistError struct{}

func (KeyNotExistError) Error() string {
	return "Key is not found"
}
