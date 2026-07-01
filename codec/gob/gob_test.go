package gob

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testPayload struct {
	Foo string
}

func TestGobCodec_EncodeDecode(t *testing.T) {
	codec := Codec{}
	value := testPayload{Foo: "bar"}

	data, err := codec.Encode(value)
	assert.NoError(t, err)

	var result testPayload

	err = codec.Decode(data, &result)
	assert.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestGobCodec_DecodeError(t *testing.T) {
	codec := Codec{}

	var result testPayload

	err := codec.Decode([]byte("invalid gob"), &result)

	assert.Error(t, err)
}
