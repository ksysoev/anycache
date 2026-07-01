package gob

import (
	"bytes"
	"encoding/gob"
)

// Codec is a codec that uses Gob encoding and decoding for values.
type Codec struct{}

// Encode encodes the given value into a Gob byte slice.
func (c Codec) Encode(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)

	if err := encoder.Encode(value); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decode decodes the given Gob data into the provided value.
func (c Codec) Decode(data []byte, value any) error {
	decoder := gob.NewDecoder(bytes.NewReader(data))
	return decoder.Decode(value)
}
