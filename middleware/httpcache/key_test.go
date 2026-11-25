package httpcache

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMiddleware(t *testing.T, opts ...Option) *Middleware {
	t.Helper()

	mw, err := New(nil, time.Minute, opts...)
	require.NoError(t, err)

	return mw
}

func TestMiddleware_Key(t *testing.T) {
	mw := newTestMiddleware(t, WithVaryHeaders("Accept"))

	get := func(target string, header http.Header) string {
		r := httptest.NewRequest(http.MethodGet, target, http.NoBody)
		for k, v := range header {
			r.Header[k] = v
		}

		key, ok := mw.Key(r)
		require.True(t, ok)

		return key
	}

	base := get("http://example.com/p?a=1&b=2", nil)

	tests := []struct {
		header http.Header
		name   string
		target string
		same   bool
	}{
		{name: "same request", target: "http://example.com/p?a=1&b=2", same: true},
		{name: "query order", target: "http://example.com/p?b=2&a=1", same: true},
		{name: "unkeyed header", target: "http://example.com/p?a=1&b=2", header: http.Header{"Accept-Language": {"de"}}, same: true},
		{name: "other host", target: "http://other.com/p?a=1&b=2"},
		{name: "other scheme", target: "https://example.com/p?a=1&b=2"},
		{name: "other path", target: "http://example.com/q?a=1&b=2"},
		{name: "other query", target: "http://example.com/p?a=1&b=3"},
		{name: "vary header", target: "http://example.com/p?a=1&b=2", header: http.Header{"Accept": {"application/json"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := get(tt.target, tt.header)

			if tt.same {
				assert.Equal(t, base, key)
			} else {
				assert.NotEqual(t, base, key)
			}
		})
	}
}

func TestMiddleware_Key_SafeForAllBackends(t *testing.T) {
	mw := newTestMiddleware(t, WithVaryHeaders("Accept"))

	r := httptest.NewRequest(http.MethodGet, "/"+strings.Repeat("x", 1000), http.NoBody)
	r.Header.Set("Accept", "text/html, application/xhtml+xml, */*")

	key, ok := mw.Key(r)
	require.True(t, ok)

	// memcache keys: at most 250 bytes, no spaces or control characters
	assert.LessOrEqual(t, len(key), 250)
	assert.NotContains(t, key, " ")
	assert.True(t, strings.HasPrefix(key, keyPrefix))
}

func TestMiddleware_Key_Bypass(t *testing.T) {
	tests := []struct {
		name   string
		method string
		opts   []Option
	}{
		{name: "POST", method: http.MethodPost},
		{
			name:   "key func opts out",
			method: http.MethodGet,
			opts:   []Option{WithKeyFunc(func(*http.Request) (string, bool) { return "", false })},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := newTestMiddleware(t, tt.opts...)

			_, ok := mw.Key(httptest.NewRequest(tt.method, "/", http.NoBody))
			assert.False(t, ok)
		})
	}
}

func TestMiddleware_Key_CustomKeyFunc(t *testing.T) {
	mw := newTestMiddleware(t, WithKeyFunc(func(r *http.Request) (string, bool) {
		return r.URL.Path, true
	}))

	k1, ok1 := mw.Key(httptest.NewRequest(http.MethodGet, "http://a.com/p?x=1", http.NoBody))
	k2, ok2 := mw.Key(httptest.NewRequest(http.MethodGet, "http://b.com/p?x=2", http.NoBody))

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, k1, k2)
}

func TestMiddleware_Key_Query(t *testing.T) {
	mw := newTestMiddleware(t)

	key := func(target string) string {
		k, ok := mw.Key(httptest.NewRequest(http.MethodGet, target, http.NoBody))
		require.True(t, ok)

		return k
	}

	tests := []struct {
		name string
		a    string
		b    string
		same bool
	}{
		{name: "parameter order", a: "/s?b=2&a=1", b: "/s?a=1&b=2", same: true},
		{name: "repeated values keep order", a: "/s?a=1&a=2", b: "/s?a=2&a=1"},
		{name: "semicolon is not dropped", a: "/s?q=1;x=evil", b: "/s"},
		{name: "semicolon differs from empty query", a: "/s?q=1;x=evil", b: "/s?"},
		{name: "invalid escape is not dropped", a: "/s?q=%zz", b: "/s"},
		{name: "raw query cannot collide with parsed", a: "/s?q=1;x=2", b: "/s?q=1%3Bx%3D2"},
		{name: "different invalid queries", a: "/s?q=%zz", b: "/s?q=%yy"},
		{name: "invalid queries ignore order", a: "/s?q=%zz&a=1", b: "/s?a=1&q=%zz", same: true},
		{name: "missing value differs from empty value", a: "/s?a", b: "/s?a="},
		{name: "plus differs from encoded space", a: "/s?q=a+b", b: "/s?q=a%20b"},
		{name: "encoded letter differs from plain letter", a: "/s?q=%41", b: "/s?q=A"},
		{name: "encoded name differs from plain name", a: "/s?%61=1", b: "/s?a=1"},
		{name: "empty parameter is kept", a: "/s?a=1&", b: "/s?a=1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.same {
				assert.Equal(t, key(tt.a), key(tt.b))
			} else {
				assert.NotEqual(t, key(tt.a), key(tt.b))
			}
		})
	}
}
