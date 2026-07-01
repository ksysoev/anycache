package avro

import (
	"errors"

	"github.com/hamba/avro/v2"
)

var errNilSchema = errors.New("avro codec: schema is nil")

// Codec is a codec that uses Avro binary marshaling and unmarshaling.
//
// Avro requires a schema to encode and decode values.
type Codec struct {
	schema avro.Schema
}

// NewCodec creates a new Avro codec from the provided schema definition.
func NewCodec(schemaDefinition string) (Codec, error) {
	schema, err := avro.Parse(schemaDefinition)
	if err != nil {
		return Codec{}, err
	}

	return Codec{schema: schema}, nil
}

// Encode encodes the given value into an Avro byte slice.
func (c Codec) Encode(value any) ([]byte, error) {
	if c.schema == nil {
		return nil, errNilSchema
	}

	return avro.Marshal(c.schema, value)
}

// Decode decodes the given Avro data into the provided value.
func (c Codec) Decode(data []byte, value any) error {
	if c.schema == nil {
		return errNilSchema
	}

	return avro.Unmarshal(c.schema, data, value)
}
