package avro

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Foo string `avro:"foo"`
}

const payloadSchema = `{
  "type": "record",
  "name": "testPayload",
  "fields": [
    {"name": "foo", "type": "string"}
  ]
}`

func TestAvroCodec_EncodeDecode(t *testing.T) {
	codec, err := NewCodec(payloadSchema)
	require.NoError(t, err)

	value := testPayload{Foo: "bar"}
	data, err := codec.Encode(value)
	assert.NoError(t, err)

	var result testPayload
	err = codec.Decode(data, &result)
	assert.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestAvroCodec_DecodeError(t *testing.T) {
	codec, err := NewCodec(payloadSchema)
	require.NoError(t, err)

	var result testPayload
	err = codec.Decode([]byte("invalid avro"), &result)

	assert.Error(t, err)
}

func TestAvroCodec_NewCodecError(t *testing.T) {
	_, err := NewCodec("{invalid schema}")
	assert.Error(t, err)
}
