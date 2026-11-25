package httpcache_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/ksysoev/anycache/middleware/httpcache"
	"github.com/ksysoev/anycache/storage/inmemory"
)

func ExampleMiddleware_Handler() {
	store, err := inmemory.New(1000)
	if err != nil {
		panic(err)
	}
	defer func() { _ = store.Close() }()

	cache := anycache.New(store)
	defer func() { _ = cache.Close() }()

	mw, err := httpcache.New(cache, time.Minute,
		httpcache.WithVaryHeaders("Accept"),
		httpcache.WithCacheStatusHeader("X-Cache"),
	)
	if err != nil {
		panic(err)
	}

	calls := 0
	products := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprintf(w, "products v%d", calls)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /products", mw.Handler(products))

	for range 2 {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/products", http.NoBody))

		fmt.Println(w.Header().Get("X-Cache"), w.Body.String())
	}

	// Output:
	// MISS products v1
	// HIT products v1
}
