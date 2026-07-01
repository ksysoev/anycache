package anycache

// Codec defines the interface for encoding and decoding values to and from byte slices.
// Implementations must be safe for concurrent use by multiple goroutines.
// Decode expects value to be a pointer to the destination type.
type Codec interface {
	Decode(data []byte, value any) error
	Encode(value any) ([]byte, error)
}
