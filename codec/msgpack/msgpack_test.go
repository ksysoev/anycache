package msgpack

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

type testPayload struct {
	Foo string `msgpack:"foo"`
}

func TestMsgpackCodec_EncodeDecode(t *testing.T) {
	codec := Codec{}
	value := testPayload{Foo: "bar"}

	data, err := codec.Encode(value)
	assert.NoError(t, err)

	var result testPayload

	err = codec.Decode(data, &result)
	assert.NoError(t, err)
	assert.Equal(t, value, result)
}

func TestMsgpackCodec_DecodeError(t *testing.T) {
	codec := Codec{}

	var result testPayload

	err := codec.Decode([]byte("invalid msgpack"), &result)

	assert.Error(t, err)
}
