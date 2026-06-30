package anycache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSONCodec_Encode(t *testing.T) {
	codec := JSONCodec{}
	value := map[string]string{"foo": "bar"}

	data, err := codec.Encode(value)

	assert.NoError(t, err)
	assert.JSONEq(t, `{"foo":"bar"}`, string(data))
}

func TestJSONCodec_Decode(t *testing.T) {
	codec := JSONCodec{}
	data := []byte(`{"foo":"bar"}`)

	var result map[string]string

	err := codec.Decode(data, &result)

	assert.NoError(t, err)
	assert.Equal(t, "bar", result["foo"])
}

func TestJSONCodec_EncodeError(t *testing.T) {
	codec := JSONCodec{}

	_, err := codec.Encode(make(chan int))

	assert.Error(t, err)
}

func TestJSONCodec_DecodeError(t *testing.T) {
	codec := JSONCodec{}

	var result map[string]string

	err := codec.Decode([]byte("{invalid json}"), &result)

	assert.Error(t, err)
}
