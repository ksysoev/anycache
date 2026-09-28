package http

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ksysoev/anycache"
	"github.com/ksysoev/anycache/storage/inmemory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMiddleware(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()

	store, err := inmemory.New(16)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	return NewMiddleware(anycache.New(store))
}

func TestGetRequestKey(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{name: "get request uses path", method: http.MethodGet, path: "/users/42", want: "/users/42"},
		{name: "post request bypasses cache", method: http.MethodPost, path: "/users/42", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path+"?q=1", http.NoBody)
			assert.Equal(t, tt.want, getRequestKey(req))
		})
	}
}

func TestMiddlewareCachesGetResponses(t *testing.T) {
	middleware := newTestMiddleware(t)

	var calls atomic.Int32

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)

		w.Header().Set("X-Cacheable", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("response-" + string('0'+call)))
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/articles", http.NoBody))

	assert.Equal(t, http.StatusCreated, first.Code)
	assert.Equal(t, "yes", first.Header().Get("X-Cacheable"))
	assert.Equal(t, "response-1", first.Body.String())

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/articles", http.NoBody))

	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, http.StatusCreated, second.Code)
	assert.Equal(t, "yes", second.Header().Get("X-Cacheable"))
	assert.Equal(t, "response-1", second.Body.String())
}

func TestMiddlewareBypassesNonGetRequests(t *testing.T) {
	middleware := newTestMiddleware(t)

	var calls atomic.Int32

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		_, _ = w.Write([]byte("response-" + string('0'+call)))
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/articles", http.NoBody))

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/articles", http.NoBody))

	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, "response-1", first.Body.String())
	assert.Equal(t, "response-2", second.Body.String())
}

func TestMiddlewareDefaultsEmptyResponseToStatusOK(t *testing.T) {
	middleware := newTestMiddleware(t)

	var calls atomic.Int32

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/empty", http.NoBody))

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/empty", http.NoBody))

	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, http.StatusOK, first.Code)
	assert.Equal(t, http.StatusOK, second.Code)
	assert.Empty(t, first.Body.String())
	assert.Empty(t, second.Body.String())
}
