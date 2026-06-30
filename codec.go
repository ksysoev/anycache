package anycache

import "encoding/json"

// Codec defines the interface for encoding and decoding values to and from byte slices.
type Codec interface {
	Decode(data []byte, value any) error
	Encode(value any) ([]byte, error)
}

// JSONCodec is a codec that uses JSON marshaling and unmarshaling for encoding and decoding values.
type JSONCodec struct{}

// Encode encodes the given value into a JSON byte slice using JSON marshaling.
func (c JSONCodec) Encode(value any) ([]byte, error) {
	return json.Marshal(value)
}

// Decode decodes the given data into the provided value using JSON unmarshaling.
func (c JSONCodec) Decode(data []byte, value any) error {
	return json.Unmarshal(data, value)
}
