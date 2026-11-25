package httpcache

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// discardWriter is a minimal ResponseWriter so benchmarks measure the middleware only.
type discardWriter struct {
	header http.Header
}

func newDiscardWriter() *discardWriter {
	return &discardWriter{header: make(http.Header)}
}

func (w *discardWriter) Header() http.Header {
	return w.header
}

func (*discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (*discardWriter) WriteHeader(int) {}

func (w *discardWriter) reset() {
	clear(w.header)
}

func benchRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/p?b=2&a=1", http.NoBody)
}

func benchBody() []byte {
	return bytes.Repeat([]byte("x"), 4<<10)
}

func benchHeader() http.Header {
	return http.Header{"Content-Type": {"application/json"}}
}

func benchHandler(body []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		_, _ = w.Write(body)
	})
}

func BenchmarkMiddleware_Hit(b *testing.B) {
	cache := newMemoryCache(b)

	mw, err := New(cache, time.Hour, WithCacheStatusHeader("X-Cache"))
	require.NoError(b, err)

	h := mw.Handler(benchHandler(benchBody()))
	r := benchRequest()
	w := newDiscardWriter()

	h.ServeHTTP(w, r) // warm the cache

	b.ReportAllocs()

	for b.Loop() {
		w.reset()
		h.ServeHTTP(w, r)
	}
}

func BenchmarkMiddleware_Bypass(b *testing.B) {
	cache := newMemoryCache(b)

	mw, err := New(cache, time.Hour)
	require.NoError(b, err)

	h := mw.Handler(benchHandler(benchBody()))
	r := httptest.NewRequest(http.MethodPost, "/p", http.NoBody)
	w := newDiscardWriter()

	b.ReportAllocs()

	for b.Loop() {
		w.reset()
		h.ServeHTTP(w, r)
	}
}

func BenchmarkEntry_Decode(b *testing.B) {
	data := encodeEntry(&response{status: http.StatusOK, header: benchHeader(), body: benchBody(), createdAt: time.Now()})

	b.ReportAllocs()

	for b.Loop() {
		if _, err := decodeEntry(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMiddleware_Key(b *testing.B) {
	mw, err := New(nil, time.Hour, WithVaryHeaders("Accept"))
	require.NoError(b, err)

	r := benchRequest()
	r.Header.Set("Accept", "application/json")

	b.ReportAllocs()

	for b.Loop() {
		mw.Key(r)
	}
}
