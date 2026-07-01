package bson

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testPayload struct {
	Foo string `bson:"foo"`
}

func TestBSONCodec_EncodeDecode(t *testing.T) {
	codec := Codec{}
	value := testPayload{Foo: "bar"}

	data, err := codec.Encode(value)
	assert.NoError(t, err)

	var result testPayload

	err = codec.Decode(data, &result)
	assert.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestBSONCodec_DecodeError(t *testing.T) {
	codec := Codec{}

	var result testPayload

	err := codec.Decode([]byte("invalid bson"), &result)

	assert.Error(t, err)
}
