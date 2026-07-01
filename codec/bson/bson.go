package bson

import mongobson "go.mongodb.org/mongo-driver/v2/bson"

// Codec is a codec that uses BSON marshaling and unmarshaling.
type Codec struct{}

// Encode encodes the given value into a BSON byte slice.
func (c Codec) Encode(value any) ([]byte, error) {
	return mongobson.Marshal(value)
}

// Decode decodes the given BSON data into the provided value.
func (c Codec) Decode(data []byte, value any) error {
	return mongobson.Unmarshal(data, value)
}
