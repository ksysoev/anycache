package httpcache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksysoev/anycache"
	"github.com/ksysoev/anycache/storage/inmemory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const statusHeader = "X-Cache"

var errStorage = errors.New("storage down")

func quietLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newMemoryCache(tb testing.TB) *anycache.Cache {
	tb.Helper()

	store, err := inmemory.New(100)
	require.NoError(tb, err)

	cache := anycache.New(store)

	tb.Cleanup(func() {
		assert.NoError(tb, cache.Close())
		assert.NoError(tb, store.Close())
	})

	return cache
}

func newHandler(t *testing.T, cache Cache, next http.Handler, opts ...Option) http.Handler {
	t.Helper()

	opts = append([]Option{WithCacheStatusHeader(statusHeader), WithLogger(quietLogger())}, opts...)

	mw, err := New(cache, time.Minute, opts...)
	require.NoError(t, err)

	return mw.Handler(next)
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	return w
}

func get(target string) *http.Request {
	return httptest.NewRequest(http.MethodGet, target, http.NoBody)
}

// countingHandler responds with "<body>-<call number>".
type countingHandler struct {
	header http.Header
	body   string
	calls  atomic.Int32
	status int
}

func (h *countingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n := h.calls.Add(1)

	for k, v := range h.header {
		w.Header()[k] = v
	}

	if h.status != 0 {
		w.WriteHeader(h.status)
	}

	_, _ = fmt.Fprintf(w, "%s-%d", h.body, n)
}

func TestNew_InvalidTTL(t *testing.T) {
	_, err := New(nil, 0)
	assert.ErrorIs(t, err, ErrInvalidTTL)
}

func TestMiddleware_CachingBehaviour(t *testing.T) {
	tests := []struct {
		req           func() *http.Request
		handler       *countingHandler
		name          string
		wantStatus2   string
		opts          []Option
		wantCalls     int32
		wantSameBody  bool
		wantHTTPCode2 int
	}{
		{
			name:          "200 is cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "ok"},
			wantCalls:     1,
			wantSameBody:  true,
			wantStatus2:   StatusHit,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "500 is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "err", status: http.StatusInternalServerError},
			wantCalls:     2,
			wantStatus2:   StatusMiss,
			wantHTTPCode2: http.StatusInternalServerError,
		},
		{
			name:          "configured 404 is cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "nf", status: http.StatusNotFound},
			opts:          []Option{WithCacheableStatus(http.StatusOK, http.StatusNotFound)},
			wantCalls:     1,
			wantSameBody:  true,
			wantStatus2:   StatusHit,
			wantHTTPCode2: http.StatusNotFound,
		},
		{
			name:          "set-cookie is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"Set-Cookie": {"s=1"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "no-store is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"Cache-Control": {"no-store"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "unsafe vary is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "v", header: http.Header{"Vary": {"Accept-Language"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase set-cookie is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"set-cookie": {"s=1"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase private is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "c", header: http.Header{"cache-control": {"private"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "lowercase unsafe vary is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "v", header: http.Header{"vary": {"Cookie"}}},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name:          "oversized body is not cached",
			req:           func() *http.Request { return get("/p") },
			handler:       &countingHandler{body: "a-very-long-body"},
			opts:          []Option{WithMaxBodySize(4)},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name: "POST bypasses",
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/p", http.NoBody)
			},
			handler:       &countingHandler{body: "post"},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
		{
			name: "authorized request bypasses",
			req: func() *http.Request {
				r := get("/p")
				r.Header.Set("Authorization", "Bearer x")

				return r
			},
			handler:       &countingHandler{body: "auth"},
			wantCalls:     2,
			wantStatus2:   StatusBypass,
			wantHTTPCode2: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(t, newMemoryCache(t), tt.handler, tt.opts...)

			first := do(h, tt.req())
			second := do(h, tt.req())

			assert.Equal(t, tt.wantCalls, tt.handler.calls.Load())
			assert.Equal(t, tt.wantHTTPCode2, second.Code)
			assert.Equal(t, tt.wantStatus2, second.Header().Get(statusHeader))

			if tt.wantSameBody {
				assert.Equal(t, first.Body.String(), second.Body.String())
			} else {
				assert.NotEqual(t, first.Body.String(), second.Body.String())
			}
		})
	}
}

func TestMiddleware_HitHeaders(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)

	data := encodeEntry(&response{
		status:    http.StatusOK,
		header:    http.Header{"Content-Type": {"text/plain"}},
		body:      []byte("cached"),
		createdAt: time.Now().Add(-10 * time.Second),
	})

	store.EXPECT().Get(mock.Anything, mock.Anything).Return(data, nil).Once()

	h := newHandler(t, anycache.New(store), http.NotFoundHandler())

	w := do(h, get("/p"))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "cached", w.Body.String())
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.Equal(t, "6", w.Header().Get("Content-Length"))
	assert.Equal(t, StatusHit, w.Header().Get(statusHeader))
	assert.Equal(t, "10", w.Header().Get("Age"))
}

func TestMiddleware_MissHeaders(t *testing.T) {
	h := newHandler(t, newMemoryCache(t), &countingHandler{body: "x"})

	w := do(h, get("/p"))

	assert.Equal(t, StatusMiss, w.Header().Get(statusHeader))
	assert.Empty(t, w.Header().Get("Age"))
}

func TestMiddleware_FailOpen(t *testing.T) {
	tests := []struct {
		setup      func(store *anycache.MockCacheStorage)
		name       string
		wantStatus string
		opts       []Option
	}{
		{
			name: "get fails",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, errStorage).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "set fails",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
				store.EXPECT().Set(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errStorage).Once()
			},
			wantStatus: StatusMiss,
		},
		{
			name: "corrupt entry is invalidated",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion}, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).Return(nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "stored pass marker is invalidated",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return(passMarker, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).Return(nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			// A newer middleware version may have written it, so it must not be deleted.
			name: "unsupported version is kept",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion + 1, 1, 2}, nil).Once()
			},
			wantStatus: StatusBypass,
		},
		{
			name: "hanging invalidate is bounded by the timeout",
			setup: func(store *anycache.MockCacheStorage) {
				store.EXPECT().Get(mock.Anything, mock.Anything).Return([]byte{entryVersion}, nil).Once()
				store.EXPECT().Del(mock.Anything, mock.Anything).
					RunAndReturn(func(ctx context.Context, _ string) error {
						<-ctx.Done()
						return ctx.Err()
					}).Once()
			},
			opts:       []Option{WithTimeout(50 * time.Millisecond)},
			wantStatus: StatusBypass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := anycache.NewMockCacheStorage(t)
			tt.setup(store)

			handler := &countingHandler{body: "fresh"}
			h := newHandler(t, anycache.New(store), handler, tt.opts...)

			w := do(h, get("/p"))

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "fresh-1", w.Body.String())
			assert.Equal(t, tt.wantStatus, w.Header().Get(statusHeader))
			assert.Equal(t, int32(1), handler.calls.Load(), "handler must run exactly once")
		})
	}
}

func TestMiddleware_Panics(t *testing.T) {
	t.Run("panic becomes 500", func(t *testing.T) {
		h := newHandler(t, newMemoryCache(t), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}))

		w := do(h, get("/p"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("ErrAbortHandler becomes 502", func(t *testing.T) {
		h := newHandler(t, newMemoryCache(t), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("partial"))

			panic(http.ErrAbortHandler)
		}))

		var w *httptest.ResponseRecorder

		assert.NotPanics(t, func() { w = do(h, get("/p")) })
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.NotContains(t, w.Body.String(), "partial")
	})
}

func TestMiddleware_HandlerRequest(t *testing.T) {
	var (
		gotValue    any
		gotINM      string
		gotPath     string
		gotDeadline bool
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotValue = r.Context().Value(ctxKey{})
		gotINM = r.Header.Get("If-None-Match")
		gotPath = r.PathValue("id")
		_, gotDeadline = r.Context().Deadline()

		_, _ = w.Write([]byte("ok"))
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", newHandler(t, newMemoryCache(t), handler))

	r := get("/items/42")
	r.Header.Set("If-None-Match", `"v1"`)
	r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, "request-value"))

	w := do(mux, r)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "request-value", gotValue)
	assert.Empty(t, gotINM)
	assert.Equal(t, "42", gotPath)
	assert.True(t, gotDeadline, "default timeout must bound the handler")
}

// blockingHandler blocks every call until release is closed.
type blockingHandler struct {
	started   chan struct{}
	release   chan struct{}
	setCookie bool
	calls     atomic.Int32
}

func newBlockingHandler(setCookie bool) *blockingHandler {
	return &blockingHandler{
		started:   make(chan struct{}, 100),
		release:   make(chan struct{}),
		setCookie: setCookie,
	}
}

func (h *blockingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := h.calls.Add(1)
	h.started <- struct{}{}

	<-h.release

	// touch the request after the leader may have gone away
	_ = r.Header.Get("Accept")
	_ = r.URL.String()

	if h.setCookie {
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    fmt.Sprintf("user-%d", n),
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})
	}

	_, _ = fmt.Fprintf(w, "body-%d", n)
}

// fetched is a client-side view of a response.
type fetched struct {
	body    string
	cookies []*http.Cookie
	status  int
}

// runConcurrent starts a leader, waits for it to enter the handler, then starts
// followers and releases the handler once they are waiting.
func runConcurrent(t *testing.T, h http.Handler, bh *blockingHandler, followers int) []*fetched {
	t.Helper()

	gate := &entryGate{}

	srv := httptest.NewServer(gate.wrap(h))
	t.Cleanup(srv.Close)

	results := make([]*fetched, followers+1)

	var wg sync.WaitGroup

	fetch := func(i int) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/p", http.NoBody)
		if !assert.NoError(t, err) {
			return
		}

		resp, err := srv.Client().Do(req)
		if !assert.NoError(t, err) {
			return
		}

		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)

		results[i] = &fetched{body: string(body), cookies: resp.Cookies(), status: resp.StatusCode}
	}

	wg.Go(func() { fetch(0) })

	<-bh.started

	for i := 1; i <= followers; i++ {
		wg.Go(func() { fetch(i) })
	}

	gate.wait(t, followers+1)
	close(bh.release)
	wg.Wait()

	return results
}

func TestMiddleware_ConcurrentRequestsRunHandlerOnce(t *testing.T) {
	bh := newBlockingHandler(false)
	h := newHandler(t, newMemoryCache(t), bh)

	results := runConcurrent(t, h, bh, 9)

	assert.Equal(t, int32(1), bh.calls.Load())

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, "body-1", res.body)
	}
}

func TestMiddleware_PrivateResponsesAreNotShared(t *testing.T) {
	const followers = 4

	bh := newBlockingHandler(true)
	h := newHandler(t, newMemoryCache(t), bh)

	results := runConcurrent(t, h, bh, followers)

	assert.Equal(t, int32(followers+1), bh.calls.Load(), "every request must run the handler itself")

	seen := map[string]bool{}

	for _, res := range results {
		require.NotNil(t, res)

		cookies := res.cookies
		require.Len(t, cookies, 1)

		assert.Equal(t, "user-"+res.body[len("body-"):], cookies[0].Value, "cookie must belong to this response")
		assert.False(t, seen[cookies[0].Value], "cookie %s leaked to another request", cookies[0].Value)

		seen[cookies[0].Value] = true
	}
}

// joinGrace covers the few instructions between a request entering the
// middleware and joining the in-flight cache call: singleflight exposes no
// waiter count to wait on.
const joinGrace = 10 * time.Millisecond

// entryGate counts requests entering the handler under test.
type entryGate struct {
	entered atomic.Int32
}

func (g *entryGate) wrap(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.entered.Add(1)
		h.ServeHTTP(w, r)
	})
}

// wait blocks until n requests entered the handler and had time to join the in-flight call.
func (g *entryGate) wait(t *testing.T, n int) {
	t.Helper()

	require.Eventually(t, func() bool { return g.entered.Load() >= int32(n) }, 5*time.Second, time.Millisecond) //nolint:gosec // small test counts
	time.Sleep(joinGrace)
}

// serveWithCancelledLeader sends a leader request and, once the handler started,
// a follower. When the follower is waiting, the leader is cancelled, then release
// is called and both responses are returned.
func serveWithCancelledLeader(t *testing.T, h http.Handler, started <-chan struct{}, release func()) (leader, follower *httptest.ResponseRecorder) {
	t.Helper()

	gate := &entryGate{}
	h = gate.wrap(h)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	leaderDone := make(chan *httptest.ResponseRecorder)
	followerDone := make(chan *httptest.ResponseRecorder)

	go func() { leaderDone <- do(h, get("/p").WithContext(ctx)) }()

	<-started

	go func() { followerDone <- do(h, get("/p")) }()

	gate.wait(t, 2)
	cancel()

	leader = <-leaderDone

	release()

	return leader, <-followerDone
}

func TestMiddleware_LeaderCancellation(t *testing.T) {
	bh := newBlockingHandler(false)
	h := newHandler(t, newMemoryCache(t), bh)

	leader, follower := serveWithCancelledLeader(t, h, bh.started, func() { close(bh.release) })

	assert.Empty(t, leader.Body.String(), "cancelled leader gets no response")
	assert.Equal(t, "body-1", follower.Body.String())
	assert.Equal(t, int32(1), bh.calls.Load())
}

func TestMiddleware_FailureLoggedWhenLeaderIsGone(t *testing.T) {
	tests := []struct {
		handler  func(started chan struct{}, release chan struct{}) http.Handler
		name     string
		wantLog  string
		opts     []Option
		wantCode int
	}{
		{
			name: "panic",
			handler: func(started, release chan struct{}) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					close(started)
					<-release
					panic("boom")
				})
			},
			wantCode: http.StatusInternalServerError,
			wantLog:  "handler panicked",
		},
		{
			name: "timeout",
			handler: func(started, _ chan struct{}) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					<-r.Context().Done()

					_, _ = w.Write([]byte("partial"))
				})
			},
			opts:     []Option{WithTimeout(500 * time.Millisecond)},
			wantCode: http.StatusGatewayTimeout,
			wantLog:  "handler did not finish in time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			started := make(chan struct{})
			release := make(chan struct{})

			opts := append([]Option{WithLogger(slog.New(slog.NewTextHandler(logs, nil)))}, tt.opts...)
			h := newHandler(t, newMemoryCache(t), tt.handler(started, release), opts...)

			_, follower := serveWithCancelledLeader(t, h, started, func() { close(release) })

			assert.Equal(t, tt.wantCode, follower.Code)
			assert.Equal(t, 1, strings.Count(logs.String(), tt.wantLog), "failure must be logged exactly once")
		})
	}
}

func TestMiddleware_PanicAfterDeadlineIsTimeout(t *testing.T) {
	logs := &syncBuffer{}

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		panic("nil result")
	})

	h := newHandler(t, newMemoryCache(t), handler,
		WithTimeout(50*time.Millisecond),
		WithLogger(slog.New(slog.NewTextHandler(logs, nil))),
	)

	w := do(h, get("/p"))

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	// The timeout is reported as soon as the context ends; the panic that follows
	// in the abandoned handler is part of the same timeout and not logged again.
	assert.Equal(t, 1, strings.Count(logs.String(), "handler did not finish in time"))
	assert.NotContains(t, logs.String(), "handler panicked")
}

func TestMiddleware_WarmUp(t *testing.T) {
	handler := &countingHandler{body: "w"}
	cache := newMemoryCache(t)
	h := newHandler(t, cache, handler, WithWarmUpTTL(time.Minute))

	first := do(h, get("/p"))
	assert.Equal(t, StatusMiss, first.Header().Get(statusHeader))

	second := do(h, get("/p"))
	assert.Equal(t, StatusWarmUp, second.Header().Get(statusHeader))
	assert.Equal(t, "w-1", second.Body.String())

	assert.Eventually(t, func() bool { return handler.calls.Load() == 2 }, time.Second, time.Millisecond)
}

func TestMiddleware_KeepsCacheMetricHook(t *testing.T) {
	store, err := inmemory.New(10)
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	var (
		mu     sync.Mutex
		states []anycache.State
	)

	cache := anycache.New(store, anycache.WithMetricHook(func(_ string, op anycache.State, _ time.Duration) {
		mu.Lock()
		defer mu.Unlock()

		states = append(states, op)
	}))

	h := newHandler(t, cache, &countingHandler{body: "m"})

	do(h, get("/p"))
	do(h, get("/p"))

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []anycache.State{anycache.CacheMiss, anycache.CacheHit}, states)
}

func TestMiddleware_Invalidate(t *testing.T) {
	handler := &countingHandler{body: "i"}
	cache := newMemoryCache(t)

	mw, err := New(cache, time.Minute, WithLogger(quietLogger()))
	require.NoError(t, err)

	h := mw.Handler(handler)

	do(h, get("/p"))

	key, ok := mw.Key(get("/p"))
	require.True(t, ok)
	require.NoError(t, cache.Invalidate(t.Context(), key))

	w := do(h, get("/p"))

	assert.Equal(t, "i-2", w.Body.String())
}

func TestMiddleware_TTLFunc(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantTTL time.Duration
	}{
		{name: "custom ttl", path: "/short", wantTTL: time.Second},
		{name: "non-positive falls back to default", path: "/default", wantTTL: time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewMockCache(t)
			cache.EXPECT().
				CacheC(mock.Anything, mock.Anything, tt.wantTTL, mock.Anything, mock.Anything).
				RunAndReturn(func(ctx context.Context, _ string, _ time.Duration, gen anycache.CacheGeneratorC, _ ...anycache.CacheItemOptions) ([]byte, error) {
					data, _, err := gen(ctx)
					return data, err
				}).
				Once()

			mw, err := New(cache, time.Minute, WithTTLFunc(func(r *http.Request) time.Duration {
				if r.URL.Path == "/short" {
					return time.Second
				}

				return 0
			}))
			require.NoError(t, err)

			w := do(mw.Handler(http.NotFoundHandler()), get(tt.path))

			assert.Equal(t, http.StatusNotFound, w.Code)
		})
	}
}

func TestMiddleware_SetFailureKeepsDeDuplication(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)
	store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
	store.EXPECT().Set(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errStorage).Once()

	bh := newBlockingHandler(false)
	h := newHandler(t, anycache.New(store), bh)

	results := runConcurrent(t, h, bh, 4)

	assert.Equal(t, int32(1), bh.calls.Load(), "followers must not rerun the handler when storing fails")

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusOK, res.status)
		assert.Equal(t, "body-1", res.body)
	}
}

func TestMiddleware_TimedOutResponseIsDiscarded(t *testing.T) {
	store := anycache.NewMockCacheStorage(t)
	store.EXPECT().Get(mock.Anything, mock.Anything).Return(nil, anycache.ErrKeyNotExists).Once()
	// no Set expectation: a timed out response must never be stored

	var calls atomic.Int32

	started := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(started)

		<-r.Context().Done()

		_, _ = w.Write([]byte(`{"partial":`))
	})

	h := newHandler(t, anycache.New(store), handler, WithTimeout(100*time.Millisecond))

	leader := make(chan *httptest.ResponseRecorder)
	follower := make(chan *httptest.ResponseRecorder)

	go func() { leader <- do(h, get("/p")) }()

	<-started

	go func() { follower <- do(h, get("/p")) }()

	for _, w := range []*httptest.ResponseRecorder{<-leader, <-follower} {
		assert.Equal(t, http.StatusGatewayTimeout, w.Code)
		assert.NotContains(t, w.Body.String(), "partial")
	}

	assert.Equal(t, int32(1), calls.Load())
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestMiddleware_PanicIsLoggedOnce(t *testing.T) {
	logs := &syncBuffer{}
	bh := newBlockingHandler(false)

	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bh.ServeHTTP(w, r)
		panic("boom")
	})

	h := newHandler(t, newMemoryCache(t), panicking, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

	results := runConcurrent(t, h, bh, 4)

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusInternalServerError, res.status)
	}

	assert.Equal(t, int32(1), bh.calls.Load())
	assert.Equal(t, 1, strings.Count(logs.String(), "handler panicked"))
}

func TestMiddleware_LeaderResponseDropsTrailers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Checksum")
		w.Header().Set("Cache-Control", "private")
		_, _ = w.Write([]byte("body"))
		w.Header().Set("X-Checksum", "abc")
		w.Header().Set(http.TrailerPrefix+"X-Late", "1")
	})

	w := do(newHandler(t, newMemoryCache(t), handler), get("/p"))

	assert.Equal(t, "body", w.Body.String())
	assert.Equal(t, "private", w.Header().Get("Cache-Control"))
	assert.Empty(t, w.Header().Values("Trailer"), "trailers are never sent, so they must not be announced")
	assert.Empty(t, w.Result().Trailer)
}

func TestMiddleware_InvalidStatusIsLogged(t *testing.T) {
	for _, code := range []int{0, 42, 1000} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			logs := &syncBuffer{}

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			})

			h := newHandler(t, newMemoryCache(t), handler, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

			w := do(h, get("/p"))

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, logs.String(), "invalid status code")
			assert.Contains(t, logs.String(), "status="+strconv.Itoa(code))
		})
	}
}

func TestMayRunLater(t *testing.T) {
	tests := []struct {
		state anycache.State
		want  bool
	}{
		{state: anycache.CacheHit, want: false},
		{state: anycache.CacheMiss, want: false},
		{state: anycache.CacheWarmUp, want: true},
		{state: anycache.CacheError, want: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.want, mayRunLater(tt.state))
		})
	}
}

func TestMiddleware_TimeoutDoesNotWaitForHandler(t *testing.T) {
	release := make(chan struct{})

	var calls atomic.Int32

	// The first call ignores its context, like a blocking call made without it.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			<-release
		}

		_, _ = w.Write([]byte("late"))
	})

	h := newHandler(t, newMemoryCache(t), handler, WithTimeout(50*time.Millisecond))

	start := time.Now()
	w := do(h, get("/p"))

	assert.Equal(t, http.StatusGatewayTimeout, w.Code)
	assert.Less(t, time.Since(start), time.Second, "the timeout must not wait for the handler")

	close(release)

	w = do(h, get("/p"))

	assert.Equal(t, StatusMiss, w.Header().Get(statusHeader), "the late response must not be stored")
	assert.Equal(t, "late", w.Body.String())
	assert.Equal(t, int32(2), calls.Load())
}

func TestMiddleware_AbortIsSharedAs502(t *testing.T) {
	logs := &syncBuffer{}
	bh := newBlockingHandler(false)

	aborting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bh.ServeHTTP(w, r)
		panic(http.ErrAbortHandler)
	})

	h := newHandler(t, newMemoryCache(t), aborting, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

	results := runConcurrent(t, h, bh, 4)

	for _, res := range results {
		require.NotNil(t, res)
		assert.Equal(t, http.StatusBadGateway, res.status)
	}

	assert.Equal(t, int32(1), bh.calls.Load())
	assert.Equal(t, 1, strings.Count(logs.String(), "handler aborted the response"))
}

func TestMiddleware_Finish(t *testing.T) {
	cancelled, cancel := context.WithTimeout(t.Context(), 0)
	defer cancel()

	tests := []struct {
		ctx     context.Context
		pe      *panicError
		check   func(t *testing.T, err error)
		name    string
		wantLog string
	}{
		{
			name: "abort after the deadline is a timeout",
			ctx:  cancelled,
			pe:   newPanicError(http.ErrAbortHandler, nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var te *timeoutError

				assert.ErrorAs(t, err, &te)
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			},
			wantLog: "handler did not finish in time",
		},
		{
			name: "abort in time is an abort",
			ctx:  t.Context(),
			pe:   newPanicError(http.ErrAbortHandler, nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var ae *abortError

				assert.ErrorAs(t, err, &ae)
			},
			wantLog: "handler aborted the response",
		},
		{
			name: "panic in time is a panic",
			ctx:  t.Context(),
			pe:   newPanicError("boom", nil),
			check: func(t *testing.T, err error) {
				t.Helper()

				var pe *panicError

				assert.ErrorAs(t, err, &pe)
			},
			wantLog: "handler panicked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &syncBuffer{}
			mw := newTestMiddleware(t, WithLogger(slog.New(slog.NewTextHandler(logs, nil))))

			resp, err := mw.finish(tt.ctx, newRecorder(0), tt.pe, "key")

			assert.Nil(t, resp)
			tt.check(t, err)
			assert.Contains(t, logs.String(), tt.wantLog)
		})
	}
}

func TestMiddleware_PrivateResponseIsReportedAsBypass(t *testing.T) {
	h := newHandler(t, newMemoryCache(t), &countingHandler{body: "c", header: http.Header{"Set-Cookie": {"s=1"}}})

	w := do(h, get("/p"))

	assert.Equal(t, "c-1", w.Body.String())
	assert.Equal(t, StatusBypass, w.Header().Get(statusHeader), "a private response is neither stored nor shared")
}
