package httpcache

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntry_RoundTrip(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)

	tests := []struct {
		header     http.Header
		wantHeader http.Header
		name       string
		body       []byte
		status     int
	}{
		{
			name:       "body and headers",
			status:     http.StatusOK,
			header:     http.Header{"Content-Type": {"text/plain"}, "X-Multi": {"a", "b"}},
			wantHeader: http.Header{"Content-Type": {"text/plain"}, "X-Multi": {"a", "b"}},
			body:       []byte("hello"),
		},
		{
			name:       "empty body and headers",
			status:     http.StatusNoContent,
			header:     http.Header{},
			wantHeader: http.Header{},
		},
		{
			name:   "drops non-storable headers",
			status: http.StatusNotFound,
			header: http.Header{
				"Connection":        {"X-Conn"},
				"X-Conn":            {"1"},
				"Transfer-Encoding": {"chunked"},
				"Content-Length":    {"5"},
				"Set-Cookie":        {"a=b"},
				"Trailer:X-T":       {"v"},
				"Etag":              {`"v1"`},
			},
			wantHeader: http.Header{"Etag": {`"v1"`}},
			body:       []byte("nope!"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := encodeEntry(&response{status: tt.status, header: tt.header, body: tt.body, createdAt: created})

			e, err := decodeEntry(data)
			require.NoError(t, err)

			assert.Equal(t, tt.status, e.status)
			assert.Equal(t, tt.wantHeader, e.header)
			assert.Equal(t, string(tt.body), string(e.body))
			assert.True(t, created.Equal(e.createdAt))
		})
	}
}

func TestDecodeEntry_Invalid(t *testing.T) {
	valid := encodeEntry(&response{
		status:    http.StatusOK,
		header:    http.Header{"Content-Type": {"text/plain"}},
		createdAt: time.Unix(1, 0),
	})

	tests := []struct {
		wantErr error
		name    string
		data    []byte
	}{
		{name: "empty", data: nil, wantErr: ErrCorruptEntry},
		{name: "pass marker", data: passMarker, wantErr: ErrUnsupportedVersion},
		{name: "unknown version", data: []byte{42, 1, 2}, wantErr: ErrUnsupportedVersion},
		{name: "truncated", data: valid[:len(valid)-3], wantErr: ErrCorruptEntry},
		{name: "huge header count", data: []byte{entryVersion, 200, 1, 2, 0xff, 0x7f}, wantErr: ErrCorruptEntry},
		{name: "invalid status", data: []byte{entryVersion, 5, 2, 0}, wantErr: ErrCorruptEntry},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeEntry(tt.data)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestIsPassMarker(t *testing.T) {
	assert.True(t, isPassMarker(passMarker))
	assert.False(t, isPassMarker(nil))
	assert.False(t, isPassMarker([]byte{entryVersion}))
	assert.False(t, isPassMarker([]byte{0, 0}))
}
