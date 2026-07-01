// Package msgpack provides a codec implementation using MessagePack for encoding and decoding caching data.
package msgpack

import "github.com/vmihailenco/msgpack/v5"

// Codec is a codec that uses MessagePack marshaling and unmarshaling.
type Codec struct{}

// Encode encodes the given value into a MessagePack byte slice.
func (c Codec) Encode(value any) ([]byte, error) {
	return msgpack.Marshal(value)
}

// Decode decodes the given MessagePack data into the provided value.
func (c Codec) Decode(data []byte, value any) error {
	return msgpack.Unmarshal(data, value)
}
