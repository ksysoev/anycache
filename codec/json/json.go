// Package json provides a codec implementation that uses JSON marshaling and unmarshaling for encoding and decoding values.
package json

import "encoding/json"

// Codec is a codec that uses JSON marshaling and unmarshaling for encoding and decoding values.
type Codec struct{}

// Encode encodes the given value into a JSON byte slice using JSON marshaling.
func (c Codec) Encode(value any) ([]byte, error) {
	return json.Marshal(value)
}

// Decode decodes the given data into the provided value using JSON unmarshaling.
func (c Codec) Decode(data []byte, value any) error {
	return json.Unmarshal(data, value)
}
